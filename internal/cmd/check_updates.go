package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"paru-overlay-updater/internal/git"
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
	HasStagedChanges(repoDir string, paths ...string) (bool, error)
	StagedChanges(repoDir string, paths ...string) ([]git.Change, error)
	Commit(repoDir, message string) error
	Push(repoDir, remote, branch string) error
	Checkout(repoDir, branch string) error
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
	RunID      string
	RunAttempt string
}

// Package represents a discovered overlay entry.
type Package struct {
	Base string
	Dir  string
}

// CheckSummary records the result of an updater run.
type CheckSummary struct {
	Checked []string
	Skipped []string // packages skipped because AUR was missing or clone failed
	Errors  []error
	PRs     []PRCreated
}

// PRCreated records a successfully created pull request.
type PRCreated struct {
	Package string
	Branch  string
	URL     string
}

// CheckUpdates discovers packages under packages/<pkgbase>/, compares each one
// against its AUR repository, and opens a pull request for every package base
// that has non-.SRCINFO changes.
func CheckUpdates(ctx context.Context, cfg CheckConfig, out io.Writer) (*CheckSummary, error) {
	if err := validateCheckConfig(cfg); err != nil {
		return nil, err
	}
	if out == nil {
		out = io.Discard
	}

	pkgs, err := DiscoverPackages(cfg.RootDir, out)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(out, "No packages discovered under packages/.")
		return &CheckSummary{}, nil
	}

	summary := &CheckSummary{}
	for _, pkg := range pkgs {
		// Ensure every package starts from the configured base branch so a
		// failure or partial state from a previous iteration cannot leak into
		// the next PR branch.
		if err := cfg.Git.Checkout(cfg.RootDir, cfg.BaseBranch); err != nil {
			summary.Errors = append(summary.Errors, err)
			continue
		}
		if err := checkPackage(ctx, cfg, pkg, out, summary); err != nil {
			summary.Errors = append(summary.Errors, err)
		}
		// Restore base again after processing so the repository is left in a
		// known state for the next package or for the operator.
		if err := cfg.Git.Checkout(cfg.RootDir, cfg.BaseBranch); err != nil {
			summary.Errors = append(summary.Errors, err)
		}
	}

	printCheckSummary(out, summary)

	if len(summary.Errors) > 0 {
		return summary, errors.Join(summary.Errors...)
	}
	return summary, nil
}

func validateCheckConfig(cfg CheckConfig) error {
	if cfg.Cloner == nil {
		return fmt.Errorf("cloner is required")
	}
	if cfg.Git == nil {
		return fmt.Errorf("git driver is required")
	}
	if cfg.GitHub == nil {
		return fmt.Errorf("GitHub client is required")
	}
	if cfg.RootDir == "" {
		return fmt.Errorf("root directory is required")
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return fmt.Errorf("owner and repository are required")
	}
	if cfg.BaseBranch == "" {
		return fmt.Errorf("base branch is required")
	}
	if cfg.RunID == "" {
		return fmt.Errorf("GitHub run ID is required")
	}
	if cfg.RunAttempt == "" {
		return fmt.Errorf("GitHub run attempt is required")
	}
	return nil
}

func checkPackage(ctx context.Context, cfg CheckConfig, pkg Package, out io.Writer, summary *CheckSummary) error {
	fmt.Fprintf(out, "Checking %s\n", pkg.Base)

	tmpDir, err := os.MkdirTemp("", "paru-overlay-aur-"+pkg.Base+"-*")
	if err != nil {
		return fmt.Errorf("prepare temp directory for %s: %w", pkg.Base, err)
	}
	defer os.RemoveAll(tmpDir)

	cloneDir := filepath.Join(tmpDir, pkg.Base)
	if err := cfg.Cloner.Clone(ctx, pkg.Base, cloneDir); err != nil {
		// AUR missing or unreachable is non-fatal: warn and continue.
		// Do not create a deletion PR in this case.
		fmt.Fprintf(out, "Warning: skipping %s: AUR repository unavailable: %v\n", pkg.Base, err)
		summary.Skipped = append(summary.Skipped, pkg.Base)
		return nil
	}

	if err := createUpdatePR(ctx, cfg, pkg, cloneDir, out, summary); err != nil {
		return fmt.Errorf("create update PR for %s: %w", pkg.Base, err)
	}
	summary.Checked = append(summary.Checked, pkg.Base)
	return nil
}

func createUpdatePR(
	ctx context.Context,
	cfg CheckConfig,
	pkg Package,
	cloneDir string,
	out io.Writer,
	summary *CheckSummary,
) error {
	branch := fmt.Sprintf("update/%s/%s-%s", pkg.Base, cfg.RunID, cfg.RunAttempt)

	if err := cfg.Git.CreateBranch(cfg.RootDir, branch, cfg.BaseBranch); err != nil {
		return err
	}

	if err := overlay.SyncRepo(cloneDir, pkg.Dir); err != nil {
		return fmt.Errorf("sync package directory: %w", err)
	}

	pkgPath := filepath.Join("packages", pkg.Base)
	if err := cfg.Git.Stage(cfg.RootDir, pkgPath); err != nil {
		return err
	}
	changed, err := cfg.Git.HasStagedChanges(cfg.RootDir, pkgPath)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(out, "  %s: up to date\n", pkg.Base)
		return nil
	}

	changes, err := cfg.Git.StagedChanges(cfg.RootDir, pkgPath)
	if err != nil {
		return err
	}
	changes = stripPackagePrefix(pkgPath, changes)

	fmt.Fprintf(out, "  %s: %d change(s) detected\n", pkg.Base, len(changes))
	for _, c := range changes {
		fmt.Fprintf(out, "    %s %s\n", c.Status, c.Path)
	}

	commitMsg := fmt.Sprintf("sync aur/%s", pkg.Base)
	if err := cfg.Git.Commit(cfg.RootDir, commitMsg); err != nil {
		return err
	}

	if err := cfg.Git.Push(cfg.RootDir, "origin", branch); err != nil {
		return err
	}

	pr := github.PullRequest{
		Title: fmt.Sprintf("Update AUR package %s", pkg.Base),
		Head:  branch,
		Base:  cfg.BaseBranch,
		Body:  buildPRBody(pkg.Base, changes),
	}
	url, err := cfg.GitHub.CreatePullRequest(ctx, cfg.Owner, cfg.Repo, pr)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "  created PR: %s\n", url)
	summary.PRs = append(summary.PRs, PRCreated{Package: pkg.Base, Branch: branch, URL: url})
	return nil
}
