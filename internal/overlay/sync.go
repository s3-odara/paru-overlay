package overlay

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Cloner abstracts how an AUR package repository is obtained. The production
// implementation shells out to git, while tests can provide a fixture cloner.
type Cloner interface {
	Clone(ctx context.Context, pkgbase, dst string) error
}

// GitCloner clones AUR package repositories with a shallow git clone.
type GitCloner struct{}

// Clone performs a shallow, checkout-less clone of
// https://aur.archlinux.org/<pkgbase>.git into dst. The checkout is deferred to
// SyncRepo so git can materialize the tree directly into the overlay package
// directory.
func (g *GitCloner) Clone(ctx context.Context, pkgbase, dst string) error {
	url := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkgbase)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--no-checkout", url, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SyncRepo materializes the tracked contents of the git repository at srcDir
// into dstDir, replacing any existing dstDir contents. The AUR-generated
// .SRCINFO file is removed after checkout so the overlay contains only the
// human-reviewable build sources.
//
// This intentionally removes dstDir before checking out instead of staging an
// atomic replacement. The updater runs in a disposable GitHub Actions checkout,
// with no concurrent readers and with git available to show/revert any partial
// result, so the simpler deletion-aware sync is easier to reason about.
func SyncRepo(srcDir, dstDir string) error {
	if err := os.MkdirAll(filepath.Dir(dstDir), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", dstDir, err)
	}
	if err := os.RemoveAll(dstDir); err != nil {
		return fmt.Errorf("remove destination %s: %w", dstDir, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create destination %s: %w", dstDir, err)
	}

	gitDir := filepath.Join(srcDir, ".git")
	if info, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("stat git directory %s: %w", gitDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("git directory %s is not a directory", gitDir)
	}

	cmd := exec.Command(
		"git",
		"--git-dir", gitDir,
		"--work-tree", dstDir,
		"checkout", "-f", "HEAD", "--", ".",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	if err := os.RemoveAll(filepath.Join(dstDir, ".SRCINFO")); err != nil {
		return fmt.Errorf("remove .SRCINFO from %s: %w", dstDir, err)
	}
	return nil
}
