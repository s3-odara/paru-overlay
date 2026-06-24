package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// DiscoverPackages reads packages/ directly under root and returns entries that
// have a regular PKGBUILD.  Entries without a PKGBUILD or with a non-regular one
// are logged as warnings and skipped.  A failure to read the packages directory
// itself aborts discovery.
func DiscoverPackages(rootDir string, out io.Writer) ([]Package, error) {
	packagesDir := filepath.Join(rootDir, "packages")
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		return nil, fmt.Errorf("read packages directory %s: %w", packagesDir, err)
	}

	var pkgs []Package
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgbase := e.Name()
		dir := filepath.Join(packagesDir, pkgbase)
		pkgbuild := filepath.Join(dir, "PKGBUILD")
		// Use Lstat so a symlink to a PKGBUILD is not accepted.
		info, err := os.Lstat(pkgbuild)
		if err != nil {
			fmt.Fprintf(out, "Warning: skipping %s: PKGBUILD not found or unreadable: %v\n", dir, err)
			continue
		}
		if !info.Mode().IsRegular() {
			fmt.Fprintf(out, "Warning: skipping %s: PKGBUILD is not a regular file\n", dir)
			continue
		}
		pkgs = append(pkgs, Package{Base: pkgbase})
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Base < pkgs[j].Base })
	return pkgs, nil
}
