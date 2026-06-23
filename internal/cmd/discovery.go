package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// DiscoverPackages reads packages/ directly under root and returns entries that
// have a readable, regular PKGBUILD.  Entries without a PKGBUILD or with an
// unreadable one are logged as warnings and skipped.  A failure to read the
// packages directory itself aborts discovery.
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
		// Verify the file is actually readable; Lstat alone does not open it.
		if err := verifyReadable(pkgbuild); err != nil {
			fmt.Fprintf(out, "Warning: skipping %s: PKGBUILD not readable: %v\n", dir, err)
			continue
		}
		pkgs = append(pkgs, Package{Base: pkgbase, Dir: dir})
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Base < pkgs[j].Base })
	return pkgs, nil
}

func verifyReadable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Attempt a small read to catch permission or device errors without
	// loading an arbitrarily large file into memory.
	buf := make([]byte, 1)
	_, err = f.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}
