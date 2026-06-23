package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"paru-overlay-updater/internal/git"
)

func stripPackagePrefix(pkgPath string, changes []git.Change) []git.Change {
	prefix := filepath.ToSlash(pkgPath) + "/"
	stripped := make([]git.Change, 0, len(changes))
	for _, c := range changes {
		c.Path = strings.TrimPrefix(filepath.ToSlash(c.Path), prefix)
		stripped = append(stripped, c)
	}
	return stripped
}

func buildPRBody(pkgbase string, changes []git.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Update AUR package `%s`.\n\n", pkgbase)

	fmt.Fprintln(&b, "### Changed files")
	for _, c := range changes {
		switch c.Status {
		case git.Added:
			fmt.Fprintf(&b, "- added: `%s`\n", c.Path)
		case git.Modified:
			fmt.Fprintf(&b, "- modified: `%s`\n", c.Path)
		case git.Deleted:
			fmt.Fprintf(&b, "- deleted: `%s`\n", c.Path)
		default:
			fmt.Fprintf(&b, "- %s: `%s`\n", c.Status, c.Path)
		}
	}

	fmt.Fprintf(&b, "\nAUR package repository: https://aur.archlinux.org/%s.git\n\n", pkgbase)

	fmt.Fprintln(&b, "- Please inspect the diff and confirm the changes are trustworthy before merging.")
	return b.String()
}

func printCheckSummary(out io.Writer, summary *CheckSummary) {
	if len(summary.PRs) == 0 && len(summary.Errors) == 0 && len(summary.Skipped) == 0 {
		fmt.Fprintln(out, "All packages are up to date.")
		return
	}
	if len(summary.PRs) > 0 {
		fmt.Fprintf(out, "\nCreated %d pull request(s):\n", len(summary.PRs))
		for _, pr := range summary.PRs {
			fmt.Fprintf(out, "  %s: %s (%s)\n", pr.Package, pr.URL, pr.Branch)
		}
	}
	if len(summary.Skipped) > 0 {
		fmt.Fprintf(out, "\nSkipped %d package(s) (AUR unavailable):\n", len(summary.Skipped))
		for _, pkg := range summary.Skipped {
			fmt.Fprintf(out, "  - %s\n", pkg)
		}
	}
	if len(summary.Errors) > 0 {
		fmt.Fprintf(out, "\nEncountered %d error(s):\n", len(summary.Errors))
		for _, err := range summary.Errors {
			fmt.Fprintf(out, "  ERR: %s\n", err)
		}
	}
}
