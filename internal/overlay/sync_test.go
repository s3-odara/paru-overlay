package overlay

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncRepo_ExcludesSrcInfoAndGit(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname=foo\n")
	writeFile(t, filepath.Join(src, ".SRCINFO"), "pkgbase = foo\n")
	writeFile(t, filepath.Join(src, "patches", "fix.patch"), "diff\n")
	commitSourceRepo(t, src)

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
		commitSourceRepo(t, src)
		dst := filepath.Join(root, "packages", name)
		if err := SyncRepo(src, dst); err != nil {
			t.Fatalf("SyncRepo %s failed: %v", name, err)
		}
	}

	assertExists(t, filepath.Join(root, "packages", "foo", "PKGBUILD"))
	assertExists(t, filepath.Join(root, "packages", "bar", "PKGBUILD"))
}

func TestSyncRepo_PreservesSymlink(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "PKGBUILD"), "pkgname=foo\n")
	if err := os.Symlink("PKGBUILD", filepath.Join(src, "rel-link")); err != nil {
		t.Fatalf("create relative symlink: %v", err)
	}
	commitSourceRepo(t, src)

	dst := t.TempDir()
	dstDir := filepath.Join(dst, "foo")
	if err := SyncRepo(src, dstDir); err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dstDir, "rel-link"))
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected rel-link to remain a symlink, mode=%s", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(dstDir, "rel-link"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "PKGBUILD" {
		t.Fatalf("unexpected symlink target: %q", target)
	}
}

func TestSyncRepo_ReplacesExistingDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "foo-aur")
	dstDir := filepath.Join(root, "packages", "foo")

	writeFile(t, filepath.Join(dstDir, "PKGBUILD"), "old PKGBUILD\n")
	writeFile(t, filepath.Join(dstDir, "removed.patch"), "old patch\n")
	writeFile(t, filepath.Join(src, "PKGBUILD"), "new PKGBUILD\n")
	commitSourceRepo(t, src)

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

func commitSourceRepo(t *testing.T, dir string) {
	t.Helper()
	runGitIn(t, dir, "init", "--quiet")
	runGitIn(t, dir, "config", "user.email", "test@example.com")
	runGitIn(t, dir, "config", "user.name", "Test")
	runGitIn(t, dir, "config", "commit.gpgsign", "false")
	runGitIn(t, dir, "add", "-A")
	runGitIn(t, dir, "commit", "-m", "fixture", "--quiet")
}

func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
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
