package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"paru-overlay-updater/internal/git"
	"paru-overlay-updater/internal/github"
)

// mockCloner copies a fixture directory into cloneDir as a git repository when
// found, or returns notExist for missing AUR repositories.
type mockCloner struct {
	fixtures map[string]string // pkgbase -> fixture source directory
	notExist map[string]bool
	cloneErr map[string]error
}

func (m *mockCloner) Clone(ctx context.Context, pkgbase, dst string) error {
	if m.notExist[pkgbase] {
		return os.ErrNotExist
	}
	if err, ok := m.cloneErr[pkgbase]; ok {
		return err
	}
	src, ok := m.fixtures[pkgbase]
	if !ok {
		return os.ErrNotExist
	}
	return copyFixture(src, dst)
}

type mockGit struct {
	branches       []string
	branchBases    []string
	commits        []string
	commitPaths    []string
	checkouts      []string
	pushes         []string
	changes        []git.Change
	remoteBranches map[string]bool // branch name -> exists on origin
	resolveHead    string          // if set, overrides ResolveHead
	resolved       string          // last value returned by ResolveHead
}

func (m *mockGit) CreateBranch(repoDir, branch, base string) error {
	m.branches = append(m.branches, branch)
	m.branchBases = append(m.branchBases, base)
	return nil
}

func (m *mockGit) Stage(repoDir string, paths ...string) error {
	if len(paths) > 0 {
		m.commitPaths = append(m.commitPaths, paths[0])
	}
	return nil
}

func (m *mockGit) HasStagedChanges(repoDir string, paths ...string) (bool, error) {
	return len(m.changes) > 0, nil
}

func (m *mockGit) StagedChanges(repoDir string, paths ...string) ([]git.Change, error) {
	return m.changes, nil
}

func (m *mockGit) Commit(repoDir, message string) error {
	m.commits = append(m.commits, message)
	return nil
}

func (m *mockGit) Push(repoDir, remote, branch string) error {
	m.pushes = append(m.pushes, branch)
	return nil
}

func (m *mockGit) Checkout(repoDir, branch string) error {
	m.checkouts = append(m.checkouts, branch)
	return nil
}

func (m *mockGit) ResolveHead(repoDir string) (string, error) {
	if m.resolveHead != "" {
		m.resolved = m.resolveHead
		return m.resolveHead, nil
	}
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--short=12", "HEAD").CombinedOutput()
	if err != nil {
		return "", err
	}
	m.resolved = strings.TrimSpace(string(out))
	return m.resolved, nil
}

func (m *mockGit) RemoteBranchExists(repoDir, remote, branch string) (bool, error) {
	return m.remoteBranches[branch], nil
}

type mockPR struct {
	calls []github.PullRequest
	url   string
	err   error
}

func (m *mockPR) CreatePullRequest(ctx context.Context, owner, repo string, pr github.PullRequest) (string, error) {
	m.calls = append(m.calls, pr)
	if m.err != nil {
		return "", m.err
	}
	if m.url != "" {
		return m.url, nil
	}
	return "https://github.com/owner/repo/pull/1", nil
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
}

func copyFixture(src, dst string) error {
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	}); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-m", "fixture", "--quiet"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New("git " + strings.Join(args, " ") + " failed: " + err.Error() + "\n" + string(out))
		}
	}
	return nil
}

func TestDiscoverPackages_SkipsInvalidEntries(t *testing.T) {
	tmp := t.TempDir()
	pkgs := filepath.Join(tmp, "packages")

	// Valid entry.
	writeFiles(t, pkgs, map[string]string{
		"foo/PKGBUILD": "pkg=foo\n",
	})
	// Missing PKGBUILD.
	if err := os.MkdirAll(filepath.Join(pkgs, "bar"), 0o755); err != nil {
		t.Fatal(err)
	}
	// PKGBUILD is a directory.
	if err := os.MkdirAll(filepath.Join(pkgs, "baz", "PKGBUILD"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got, err := DiscoverPackages(tmp, &out)
	if err != nil {
		t.Fatalf("DiscoverPackages failed: %v", err)
	}
	if len(got) != 1 || got[0].Base != "foo" {
		t.Fatalf("expected only foo, got %v", got)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "bar") || !strings.Contains(outStr, "baz") {
		t.Fatalf("output should warn about skipped entries, got:\n%s", outStr)
	}
}

func TestDiscoverPackages_SkipsSymlinkAndUnreadablePKGBUILD(t *testing.T) {
	tmp := t.TempDir()
	pkgs := filepath.Join(tmp, "packages")

	// Valid entry.
	writeFiles(t, pkgs, map[string]string{
		"foo/PKGBUILD": "pkg=foo\n",
	})

	// PKGBUILD is a symlink.
	barDir := filepath.Join(pkgs, "bar")
	if err := os.MkdirAll(barDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(pkgs, "foo", "PKGBUILD"), filepath.Join(barDir, "PKGBUILD")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// PKGBUILD exists but is unreadable.
	bazDir := filepath.Join(pkgs, "baz")
	writeFiles(t, pkgs, map[string]string{
		"baz/PKGBUILD": "pkg=baz\n",
	})
	if err := os.Chmod(filepath.Join(bazDir, "PKGBUILD"), 0o000); err != nil {
		t.Fatalf("chmod unreadable: %v", err)
	}
	defer os.Chmod(filepath.Join(bazDir, "PKGBUILD"), 0o644)

	var out strings.Builder
	got, err := DiscoverPackages(tmp, &out)
	if err != nil {
		t.Fatalf("DiscoverPackages failed: %v", err)
	}
	if len(got) != 1 || got[0].Base != "foo" {
		t.Fatalf("expected only foo, got %v", got)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "bar") || !strings.Contains(outStr, "not a regular file") {
		t.Fatalf("output should warn about symlink bar, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "baz") || !strings.Contains(outStr, "not readable") {
		t.Fatalf("output should warn about unreadable baz, got:\n%s", outStr)
	}
}

func TestCheckUpdates_SkipsAURMissing(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\n",
	})

	g := &mockGit{}
	pr := &mockPR{}
	cfg := CheckConfig{
		Cloner:     &mockCloner{notExist: map[string]bool{"foo": true}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	var out strings.Builder
	summary, err := CheckUpdates(context.Background(), cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Skipped) != 1 || summary.Skipped[0] != "foo" {
		t.Fatalf("expected foo skipped, got %v", summary.Skipped)
	}
	if len(g.branches) != 0 {
		t.Fatalf("expected no branch created for missing AUR package")
	}
	if len(pr.calls) != 0 {
		t.Fatalf("expected no PR created for missing AUR package")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "AUR repository unavailable") {
		t.Fatalf("output should warn about missing AUR repo, got:\n%s", out.String())
	}
	if !strings.Contains(outStr, "Skipped 1 package") {
		t.Fatalf("summary should report skipped package, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "All packages are up to date") {
		t.Fatalf("summary should not call skipped package up to date, got:\n%s", outStr)
	}
}

func TestCheckUpdates_NoPRForSrcInfoOnly(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\npkgver=1\n",
		"foo/.SRCINFO": "old\n",
	})
	aurFixture := t.TempDir()
	writeFiles(t, aurFixture, map[string]string{
		"PKGBUILD": "pkg=foo\npkgver=1\n",
		".SRCINFO": "new\n",
	})

	g := &mockGit{}
	pr := &mockPR{}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": aurFixture}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	var out strings.Builder
	summary, err := CheckUpdates(context.Background(), cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Checked) != 1 {
		t.Fatalf("expected foo checked, got %v", summary.Checked)
	}
	if len(pr.calls) != 0 {
		t.Fatalf("expected no PR for .SRCINFO-only difference")
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("output should say up to date, got:\n%s", out.String())
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("git", "init", "--quiet")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-m", "initial", "--quiet")
	run("git", "branch", "-m", "main")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// noopPushGit delegates all git operations to ExecDriver but records pushes
// instead of contacting a remote. This lets integration tests verify real git
// state without requiring network access.
type noopPushGit struct {
	*git.ExecDriver
	pushes []string
}

func (n *noopPushGit) Push(repoDir, remote, branch string) error {
	n.pushes = append(n.pushes, branch)
	return nil
}

func TestCheckUpdates_MultiplePackagesBranchIsolation(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	// Two packages with old content.
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\npkgver=1\n",
		"bar/PKGBUILD": "pkg=bar\npkgver=1\n",
	})
	runGit(t, tmp, "add", "packages")
	runGit(t, tmp, "commit", "-m", "add packages", "--quiet")

	// Provide an origin remote so RemoteBranchExists can query it without
	// contacting the network. The bare repo starts with no branches, so both
	// packages will proceed to create their PRs.
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "--quiet")
	runGit(t, tmp, "remote", "add", "origin", origin)

	// AUR fixtures with updates for both packages.
	fooAUR := t.TempDir()
	writeFiles(t, fooAUR, map[string]string{"PKGBUILD": "pkg=foo\npkgver=2\n"})
	barAUR := t.TempDir()
	writeFiles(t, barAUR, map[string]string{"PKGBUILD": "pkg=bar\npkgver=2\n"})

	pr := &mockPR{}
	g := &noopPushGit{ExecDriver: &git.ExecDriver{}}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": fooAUR, "bar": barAUR}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	summary, err := CheckUpdates(context.Background(), cfg, io.Discard)
	if err != nil {
		t.Fatalf("CheckUpdates failed: %v", err)
	}
	if len(summary.PRs) != 2 {
		t.Fatalf("expected 2 PRs, got %v", summary.PRs)
	}

	// Verify each update branch only contains its own package's changes.
	for _, pkg := range []string{"foo", "bar"} {
		var branch string
		for _, pr := range summary.PRs {
			if pr.Package == pkg {
				branch = pr.Branch
				break
			}
		}
		if branch == "" {
			t.Fatalf("missing branch for %s", pkg)
		}

		out, err := exec.Command("git", "-C", tmp, "diff", "main.."+branch, "--name-only").CombinedOutput()
		if err != nil {
			t.Fatalf("diff %s: %v\n%s", branch, err, out)
		}
		files := strings.Fields(string(out))
		want := filepath.ToSlash(filepath.Join("packages", pkg, "PKGBUILD"))
		if len(files) != 1 || files[0] != want {
			t.Fatalf("%s should only contain %s, got %v", branch, want, files)
		}
	}
}

func TestCheckUpdates_CreatesPRWithAddModifyDelete(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD":    "pkg=foo\npkgver=1\n",
		"foo/foo.install": "post_install(){}\n",
		"foo/fix.patch":   "old\n",
		"foo/helper.sh":   "#!/bin/sh\n",
	})
	aurFixture := t.TempDir()
	writeFiles(t, aurFixture, map[string]string{
		"PKGBUILD":    "pkg=foo\npkgver=2\n",
		"foo.install": "post_install(){}\n#note\n",
		"new-script":  "echo hi\n",
	})

	g := &mockGit{changes: []git.Change{
		{Path: "packages/foo/PKGBUILD", Status: git.Modified},
		{Path: "packages/foo/foo.install", Status: git.Modified},
		{Path: "packages/foo/fix.patch", Status: git.Deleted},
		{Path: "packages/foo/helper.sh", Status: git.Deleted},
		{Path: "packages/foo/new-script", Status: git.Added},
	}}
	pr := &mockPR{}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": aurFixture}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	var out strings.Builder
	summary, err := CheckUpdates(context.Background(), cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.PRs) != 1 {
		t.Fatalf("expected 1 PR, got %v", summary.PRs)
	}
	wantBranch := "update/foo/" + g.resolved
	if len(g.branches) != 1 || g.branches[0] != wantBranch {
		t.Fatalf("unexpected branch name: got %v, want %s", g.branches, wantBranch)
	}
	if len(g.commits) != 1 || g.commits[0] != "sync aur/foo" {
		t.Fatalf("unexpected commit: %v", g.commits)
	}
	if len(g.commitPaths) != 1 || g.commitPaths[0] != "packages/foo" {
		t.Fatalf("expected path-scoped staging for packages/foo, got %v", g.commitPaths)
	}
	if len(g.branchBases) != 1 || g.branchBases[0] != "main" {
		t.Fatalf("expected branch from main, got %v", g.branchBases)
	}

	prCall := pr.calls[0]
	if prCall.Title != "Update AUR package foo" {
		t.Fatalf("unexpected title: %q", prCall.Title)
	}
	if prCall.Head != g.branches[0] {
		t.Fatalf("PR head should match branch")
	}
	if prCall.Base != "main" {
		t.Fatalf("unexpected base: %q", prCall.Base)
	}
	body := prCall.Body
	for _, want := range []string{
		"foo",
		"added: `new-script`",
		"modified: `PKGBUILD`",
		"modified: `foo.install`",
		"deleted: `fix.patch`",
		"deleted: `helper.sh`",
		"https://aur.archlinux.org/foo.git",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("PR body missing %q:\n%s", want, body)
		}
	}

	// Verify the overlay directory was synced (deleted files removed).
	if _, err := os.Stat(filepath.Join(tmp, "packages", "foo", "fix.patch")); !os.IsNotExist(err) {
		t.Fatalf("deleted AUR file should be removed from overlay")
	}
	assertFile(t, filepath.Join(tmp, "packages", "foo", "new-script"), "echo hi\n")
}

func TestCheckUpdates_PropagatesPRError(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\n",
	})
	aurFixture := t.TempDir()
	writeFiles(t, aurFixture, map[string]string{
		"PKGBUILD": "pkg=foo\npkgver=2\n",
	})

	g := &mockGit{changes: []git.Change{{Path: "packages/foo/PKGBUILD", Status: git.Modified}}}
	pr := &mockPR{err: errors.New("pull-requests: write required")}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": aurFixture}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	_, err := CheckUpdates(context.Background(), cfg, io.Discard)
	if err == nil {
		t.Fatal("expected error when PR creation fails")
	}
	if !strings.Contains(err.Error(), "pull-requests: write") {
		t.Fatalf("expected actionable PR error, got: %v", err)
	}
}

func TestCheckUpdates_SkipsDuplicateRemoteBranch(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\npkgver=1\n",
	})
	aurFixture := t.TempDir()
	writeFiles(t, aurFixture, map[string]string{
		"PKGBUILD": "pkg=foo\npkgver=2\n",
	})

	g := &mockGit{
		changes:        []git.Change{{Path: "packages/foo/PKGBUILD", Status: git.Modified}},
		resolveHead:    "deadbeef1234",
		remoteBranches: map[string]bool{"update/foo/deadbeef1234": true},
	}
	pr := &mockPR{}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": aurFixture}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	var out strings.Builder
	summary, err := CheckUpdates(context.Background(), cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.PRs) != 0 {
		t.Fatalf("expected no PR for duplicate remote branch, got %v", summary.PRs)
	}
	if len(g.commits) != 0 || len(g.pushes) != 0 {
		t.Fatalf("expected no commit/push for duplicate branch, commits=%v pushes=%v", g.commits, g.pushes)
	}
	if len(g.branches) != 0 {
		t.Fatalf("expected duplicate check to skip before local branch creation, got %v", g.branches)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Fatalf("output should report duplicate branch skip, got:\n%s", out.String())
	}
}

func TestCheckUpdates_CreatesNewPRWhenOlderBranchExists(t *testing.T) {
	tmp := t.TempDir()
	writeFiles(t, filepath.Join(tmp, "packages"), map[string]string{
		"foo/PKGBUILD": "pkg=foo\npkgver=1\n",
	})
	aurFixture := t.TempDir()
	writeFiles(t, aurFixture, map[string]string{
		"PKGBUILD": "pkg=foo\npkgver=2\n",
	})

	g := &mockGit{
		changes:        []git.Change{{Path: "packages/foo/PKGBUILD", Status: git.Modified}},
		resolveHead:    "newsha123456",
		remoteBranches: map[string]bool{"update/foo/oldsha123456": true},
	}
	pr := &mockPR{}
	cfg := CheckConfig{
		Cloner:     &mockCloner{fixtures: map[string]string{"foo": aurFixture}},
		Git:        g,
		GitHub:     pr,
		RootDir:    tmp,
		Owner:      "owner",
		Repo:       "repo",
		BaseBranch: "main",
	}

	summary, err := CheckUpdates(context.Background(), cfg, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.PRs) != 1 {
		t.Fatalf("expected 1 PR, got %v", summary.PRs)
	}
	wantBranch := "update/foo/newsha123456"
	if summary.PRs[0].Branch != wantBranch {
		t.Fatalf("expected branch %s, got %s", wantBranch, summary.PRs[0].Branch)
	}
	if len(g.commits) != 1 || len(g.pushes) != 1 {
		t.Fatalf("expected commit and push for new branch, commits=%v pushes=%v", g.commits, g.pushes)
	}
}

func TestBuildPRBody_ContainsRequiredSections(t *testing.T) {
	changes := []git.Change{
		{Path: "PKGBUILD", Status: git.Modified},
		{Path: "foo.install", Status: git.Added},
		{Path: "old.patch", Status: git.Deleted},
	}
	body := buildPRBody("foo", changes)
	for _, want := range []string{
		"Update AUR package `foo`",
		"modified: `PKGBUILD`",
		"added: `foo.install`",
		"deleted: `old.patch`",
		"https://aur.archlinux.org/foo.git",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s: got %q, want %q", path, got, want)
	}
}
