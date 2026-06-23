package overlay

import (
	"context"
	"fmt"
	"io"
	"io/fs"
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

// Clone performs a shallow clone of https://aur.archlinux.org/<pkgbase>.git
// into dst. It captures git output on failure so that operators can diagnose
// missing or unavailable repositories.
func (g *GitCloner) Clone(ctx context.Context, pkgbase, dst string) error {
	url := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkgbase)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", url, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SyncRepo copies the contents of srcDir into dstDir, replacing any existing
// dstDir contents. Files named .SRCINFO and anything under a .git/ directory
// are skipped so the overlay contains only the human-reviewable build sources.
//
// This intentionally removes dstDir before copying instead of staging an
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

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if isExcluded(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// WalkDir uses lstat semantics, so d.IsDir() is true only for real
		// directories, not symlinks to directories. Reject symlinks and any
		// other special files to avoid copying untrusted AUR symlink targets.
		if !d.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to copy non-regular file %s (mode %s)", path, info.Mode())
		}

		dstPath := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}
		return copyFile(path, dstPath, info.Mode())
	})
}

// isExcluded reports whether a path relative to the repository root should be
// omitted from the overlay. The AUR-generated .SRCINFO and the git metadata
// directory are both excluded at this layer.
func isExcluded(rel string, isDir bool) bool {
	if rel == ".SRCINFO" {
		return true
	}
	if rel == ".git" {
		return true
	}
	if strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
		return true
	}
	return false
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", dst, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}
