package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// ExecDriver shells out to git for production updater runs.
type ExecDriver struct{}

func runGit(args ...string) ([]byte, error) {
	return exec.Command("git", args...).CombinedOutput()
}

func (e *ExecDriver) CreateBranch(repoDir, branch, base string) error {
	out, err := runGit("-C", repoDir, "checkout", "-b", branch, base)
	if err != nil {
		return fmt.Errorf("create branch %q from %q: %w\n%s", branch, base, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Stage(repoDir string, paths ...string) error {
	args := []string{"-C", repoDir, "add", "-A", "--"}
	args = append(args, paths...)
	out, err := runGit(args...)
	if err != nil {
		return fmt.Errorf("stage changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) StagedFiles(repoDir string, paths ...string) ([]string, error) {
	args := []string{"-C", repoDir, "diff", "--cached", "--name-only"}
	if len(paths) > 0 {
		// Strip the package prefix from output paths; the trailing pathspec
		// (after --) restricts the diff to the package directory.
		args = append(args, "--relative="+paths[0])
	}
	args = append(args, "--")
	args = append(args, paths...)
	out, err := runGit(args...)
	if err != nil {
		return nil, fmt.Errorf("list staged changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return splitLines(string(out)), nil
}

func splitLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (e *ExecDriver) Commit(repoDir, message string) error {
	out, err := runGit("-C", repoDir, "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("commit changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Push(repoDir, remote, branch string) error {
	out, err := runGit("-C", repoDir, "push", remote, branch)
	if err != nil {
		return fmt.Errorf("push branch %q to %q: %w\n%s\n(ensure the workflow job has permission 'contents: write' and the token is authorized to push to this repository)", branch, remote, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Checkout(repoDir, branch string) error {
	out, err := runGit("-C", repoDir, "checkout", branch)
	if err != nil {
		return fmt.Errorf("checkout %q: %w\n%s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ResolveHead returns the short (12 character) SHA of the current HEAD of the
// repository at repoDir. It is used to fingerprint the AUR revision so the
// updater can name PR branches deterministically and avoid duplicate proposals.
func (e *ExecDriver) ResolveHead(repoDir string) (string, error) {
	out, err := runGit("-C", repoDir, "rev-parse", "--short=12", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD in %q: %w\n%s", repoDir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// RemoteBranchExists reports whether the named branch exists on the given
// remote. git ls-remote returns exit code 2 when no matching refs are found,
// which is treated as "does not exist". Any other failure (e.g. the remote is
// unreachable) is returned as an error so the caller does not silently skip
// updates because of a configuration problem.
func (e *ExecDriver) RemoteBranchExists(repoDir, remote, branch string) (bool, error) {
	ref := fmt.Sprintf("refs/heads/%s", branch)
	out, err := runGit("-C", repoDir, "ls-remote", "--exit-code", "--heads", remote, ref)
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		return false, nil
	}
	return false, fmt.Errorf("check remote branch %q on %q: %w\n%s", branch, remote, err, strings.TrimSpace(string(out)))
}
