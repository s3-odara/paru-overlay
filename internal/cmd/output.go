package cmd

import (
	"fmt"
	"strings"
)

func buildPRBody(pkgbase string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Update AUR package `%s`.\n\n", pkgbase)

	fmt.Fprintln(&b, "### Changed files")
	for _, f := range files {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}

	fmt.Fprintf(&b, "\nAUR package repository: https://aur.archlinux.org/%s.git\n\n", pkgbase)

	fmt.Fprintln(&b, "- Please inspect the diff and confirm the changes are trustworthy before merging.")
	return b.String()
}
