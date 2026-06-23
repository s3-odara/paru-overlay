package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// ChangeStatus describes how git reports a path in a diff.
type ChangeStatus string

const (
	Added    ChangeStatus = "added"
	Modified ChangeStatus = "modified"
	Deleted  ChangeStatus = "deleted"
)

// Change records one changed path from git diff.
type Change struct {
	Path   string
	Status ChangeStatus
}

// ExecDriver shells out to git for production updater runs.
type ExecDriver struct{}

func (e *ExecDriver) CreateBranch(repoDir, branch, base string) error {
	out, err := exec.Command("git", "-C", repoDir, "checkout", "-b", branch, base).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create branch %q from %q: %w\n%s", branch, base, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Stage(repoDir string, paths ...string) error {
	args := []string{"-C", repoDir, "add", "-A", "--"}
	args = append(args, paths...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stage changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) HasStagedChanges(repoDir string, paths ...string) (bool, error) {
	args := []string{"-C", repoDir, "diff", "--cached", "--quiet", "--exit-code", "--"}
	args = append(args, paths...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("check staged diff: %w\n%s", err, strings.TrimSpace(string(out)))
}

func (e *ExecDriver) StagedChanges(repoDir string, paths ...string) ([]Change, error) {
	args := []string{"-C", repoDir, "diff", "--cached", "--name-status", "--"}
	args = append(args, paths...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list staged changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return parseNameStatus(string(out)), nil
}

func (e *ExecDriver) Commit(repoDir, message string) error {
	out, err := exec.Command("git", "-C", repoDir, "commit", "-m", message).CombinedOutput()
	if err != nil {
		return fmt.Errorf("commit changes: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Push(repoDir, remote, branch string) error {
	out, err := exec.Command("git", "-C", repoDir, "push", remote, branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("push branch %q to %q: %w\n%s\n(ensure the workflow job has permission 'contents: write' and the token is authorized to push to this repository)", branch, remote, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *ExecDriver) Checkout(repoDir, branch string) error {
	out, err := exec.Command("git", "-C", repoDir, "checkout", branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout %q: %w\n%s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func parseNameStatus(output string) []Change {
	var changes []Change
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := statusFromGit(fields[0])
		if status == "" {
			continue
		}
		path := fields[len(fields)-1]
		changes = append(changes, Change{Path: path, Status: status})
	}
	return changes
}

func statusFromGit(status string) ChangeStatus {
	switch status[0] {
	case 'A':
		return Added
	case 'M', 'T', 'R', 'C':
		return Modified
	case 'D':
		return Deleted
	default:
		return ""
	}
}
