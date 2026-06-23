package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncRepo_ExcludesSrcInfoAndGit(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname=foo\n")
	writeFile(t, filepath.Join(src, ".SRCINFO"), "pkgbase = foo\n")
	writeFile(t, filepath.Join(src, ".git", "config"), "[core]\n")
	writeFile(t, filepath.Join(src, "patches", "fix.patch"), "diff\n")

	dst := t.TempDir()
	dstDir := filepath.Join(dst, "foo")
	if err := SyncRepo(src, dstDir); err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
	}

	assertExists(t, filepath.Join(dstDir, "PKGBUILD"))
	assertExists(t, filepath.Join(dstDir, "patches", "fix.patch"))
	assertNotExists(t, filepath.Join(dstDir, ".SRCINFO"))
	assertNotExists(t, filepath.Join(dstDir, ".git"))
}

func TestSyncRepo_MultiplePackages(t *testing.T) {
	root := t.TempDir()

	for _, name := range []string{"foo", "bar"} {
		src := filepath.Join(root, name+"-aur")
		writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname="+name+"\n")
		dst := filepath.Join(root, "packages", name)
		if err := SyncRepo(src, dst); err != nil {
			t.Fatalf("SyncRepo %s failed: %v", name, err)
		}
	}

	assertExists(t, filepath.Join(root, "packages", "foo", "PKGBUILD"))
	assertExists(t, filepath.Join(root, "packages", "bar", "PKGBUILD"))
}

func TestSyncRepo_RejectsAbsoluteSymlink(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname=foo\n")

	absTarget := filepath.Join(t.TempDir(), "target")
	writeFile(t, absTarget, "secret\n")
	if err := os.Symlink(absTarget, filepath.Join(src, "abs-link")); err != nil {
		t.Fatalf("create absolute symlink: %v", err)
	}

	dst := t.TempDir()
	err := SyncRepo(src, filepath.Join(dst, "foo"))
	if err == nil {
		t.Fatal("expected SyncRepo to reject absolute symlink")
	}
	if !strings.Contains(err.Error(), "abs-link") {
		t.Fatalf("error should mention abs-link, got: %v", err)
	}
}

func TestSyncRepo_RejectsRelativeSymlink(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname=foo\n")

	if err := os.Symlink("PKGBUILD", filepath.Join(src, "rel-link")); err != nil {
		t.Fatalf("create relative symlink: %v", err)
	}

	dst := t.TempDir()
	err := SyncRepo(src, filepath.Join(dst, "foo"))
	if err == nil {
		t.Fatal("expected SyncRepo to reject relative symlink")
	}
	if !strings.Contains(err.Error(), "rel-link") {
		t.Fatalf("error should mention rel-link, got: %v", err)
	}
}

func TestSyncRepo_ReplacesExistingDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "foo-aur")
	dstDir := filepath.Join(root, "packages", "foo")

	writeFile(t, filepath.Join(dstDir, "PKGBUILD"), "old PKGBUILD\n")
	writeFile(t, filepath.Join(dstDir, "removed.patch"), "old patch\n")
	writeFile(t, filepath.Join(src, "PKGBUILD"), "new PKGBUILD\n")

	if err := SyncRepo(src, dstDir); err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dstDir, "PKGBUILD"))
	if err != nil {
		t.Fatalf("read synced PKGBUILD: %v", err)
	}
	if string(got) != "new PKGBUILD\n" {
		t.Fatalf("destination was not replaced: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "removed.patch")); !os.IsNotExist(err) {
		t.Fatalf("stale destination file should be removed, got err=%v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist", path)
	}
}
