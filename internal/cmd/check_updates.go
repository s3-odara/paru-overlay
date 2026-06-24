package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"paru-overlay-updater/internal/github"
	"paru-overlay-updater/internal/overlay"
)

// PRClient is the subset of the GitHub client used by the updater.  Tests
// can provide a fake implementation.
type PRClient interface {
	CreatePullRequest(ctx context.Context, owner, repo string, pr github.PullRequest) (string, error)
}

// GitDriver is the subset of git operations needed by the updater.
type GitDriver interface {
	CreateBranch(repoDir, branch, base string) error
	Stage(repoDir string, paths ...string) error
	StagedFiles(repoDir string, paths ...string) ([]string, error)
	Commit(repoDir, message string) error
	Push(repoDir, remote, branch string) error
	Checkout(repoDir, branch string) error
	ResolveHead(repoDir string) (string, error)
	RemoteBranchExists(repoDir, remote, branch string) (bool, error)
}

// CheckConfig wires the external dependencies of the updater.
type CheckConfig struct {
	Cloner     overlay.Cloner
	Git        GitDriver
	GitHub     PRClient
	RootDir    string
	Owner      string
	Repo       string
	BaseBranch string
}

// Package represents a discovered overlay entry.
type Package struct {
	Base string
}

// CheckUpdates discovers packages under packages/<pkgbase>/, compares each one
// against its AUR repository, and opens a pull request for every package base
// that has non-.SRCINFO changes.
func CheckUpdates(ctx context.Context, cfg CheckConfig, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	pkgs, err := DiscoverPackages(cfg.RootDir, out)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(out, "No packages discovered under packages/.")
		return nil
	}

	var errs []error
	created := 0
	skipped := 0

	// Start from the configured base branch so a failure or partial state from a
	// previous run cannot leak into the first PR branch.
	if err := cfg.Git.Checkout(cfg.RootDir, cfg.BaseBranch); err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}

	for _, pkg := range pkgs {
		createdPR, skippedPkg, err := checkPackage(ctx, cfg, pkg, out)
		if err != nil {
			errs = append(errs, err)
		}
		if createdPR {
			created++
		}
		if skippedPkg {
			skipped++
		}
		// Restore base after processing so the repository is left in a known
		// state for the next package or for the operator.
		if err := cfg.Git.Checkout(cfg.RootDir, cfg.BaseBranch); err != nil {
			errs = append(errs, err)
		}
	}

	if created == 0 && len(errs) == 0 && skipped == 0 {
		fmt.Fprintln(out, "All packages are up to date.")
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func checkPackage(ctx context.Context, cfg CheckConfig, pkg Package, out io.Writer) (created, skipped bool, err error) {
	fmt.Fprintf(out, "Checking %s\n", pkg.Base)

	tmpDir, err := os.MkdirTemp("", "paru-overlay-aur-"+pkg.Base+"-*")
	if err != nil {
		return false, false, fmt.Errorf("prepare temp directory for %s: %w", pkg.Base, err)
	}
	defer os.RemoveAll(tmpDir)

	cloneDir := filepath.Join(tmpDir, pkg.Base)
	if err := cfg.Cloner.Clone(ctx, pkg.Base, cloneDir); err != nil {
		// AUR missing or unreachable is non-fatal: warn and continue.
		// Do not create a deletion PR in this case.
		fmt.Fprintf(out, "Warning: skipping %s: AUR repository unavailable: %v\n", pkg.Base, err)
		return false, true, nil
	}

	created, err = createUpdatePR(ctx, cfg, pkg, cloneDir, out)
	if err != nil {
		return false, false, fmt.Errorf("create update PR for %s: %w", pkg.Base, err)
	}
	return created, false, nil
}

func createUpdatePR(
	ctx context.Context,
	cfg CheckConfig,
	pkg Package,
	cloneDir string,
	out io.Writer,
) (bool, error) {
	// Fingerprint this AUR revision so the PR branch name is deterministic and
	// can be compared against already-pushed update branches.
	headSHA, err := cfg.Git.ResolveHead(cloneDir)
	if err != nil {
		return false, fmt.Errorf("resolve AUR HEAD for %s: %w", pkg.Base, err)
	}
	branch := fmt.Sprintf("update/%s/%s", pkg.Base, headSHA)

	// If an update branch for this exact AUR revision already exists on the
	// remote, another run already proposed (or is proposing) the same changes.
	// Skip creating a duplicate PR, but do not try to update the existing branch.
	exists, err := cfg.Git.RemoteBranchExists(cfg.RootDir, "origin", branch)
	if err != nil {
		return false, err
	}
	if exists {
		fmt.Fprintf(out, "  %s: update branch %s already exists; skipping duplicate\n", pkg.Base, branch)
		return false, nil
	}

	if err := cfg.Git.CreateBranch(cfg.RootDir, branch, cfg.BaseBranch); err != nil {
		return false, err
	}

	pkgDir := filepath.Join(cfg.RootDir, "packages", pkg.Base)
	if err := overlay.SyncRepo(cloneDir, pkgDir); err != nil {
		return false, fmt.Errorf("sync package directory: %w", err)
	}

	pkgPath := filepath.Join("packages", pkg.Base)
	if err := cfg.Git.Stage(cfg.RootDir, pkgPath); err != nil {
		return false, err
	}

	files, err := cfg.Git.StagedFiles(cfg.RootDir, pkgPath)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		fmt.Fprintf(out, "  %s: up to date\n", pkg.Base)
		return false, nil
	}

	fmt.Fprintf(out, "  %s: %d change(s) detected\n", pkg.Base, len(files))
	for _, f := range files {
		fmt.Fprintf(out, "    %s\n", f)
	}

	commitMsg := fmt.Sprintf("sync aur/%s", pkg.Base)
	if err := cfg.Git.Commit(cfg.RootDir, commitMsg); err != nil {
		return false, err
	}

	if err := cfg.Git.Push(cfg.RootDir, "origin", branch); err != nil {
		return false, err
	}

	pr := github.PullRequest{
		Title: fmt.Sprintf("Update AUR package %s", pkg.Base),
		Head:  branch,
		Base:  cfg.BaseBranch,
		Body:  buildPRBody(pkg.Base, files),
	}
	url, err := cfg.GitHub.CreatePullRequest(ctx, cfg.Owner, cfg.Repo, pr)
	if err != nil {
		return false, err
	}

	fmt.Fprintf(out, "  created PR: %s\n", url)
	return true, nil
}
