# paru-overlay

s3-odara's AUR overlay

## paru.conf

```ini
[paru-overlay]
Path = /home/user/git/paru-overlay/packages
Url = https://github.com/s3-odara/paru-overlay
Depth = 1
GenerateSrcinfo = true
```

`GenerateSrcinfo = true` lets `paru` generate `.SRCINFO` locally when it scans
the repository. Keep generated `.SRCINFO` files uncommitted.

## Adding PKGBUILD

```sh
pkgbase=example-package
tmp=$(mktemp -d)
git clone "https://aur.archlinux.org/${pkgbase}.git" "$tmp/$pkgbase"
mkdir -p "packages/$pkgbase"
find "packages/$pkgbase" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -a "$tmp/$pkgbase/." "packages/$pkgbase/"
rm -rf "packages/$pkgbase/.git"
rm -f  "packages/$pkgbase/.SRCINFO"
rm -rf "$tmp"
git add "packages/$pkgbase"
git commit -m "add aur/$pkgbase"
```

or locally

```sh
mkdir -p packages/example-package
cp PKGBUILD *.install *.patch packages/example-package/ 2>/dev/null || true
git add packages/example-package
git commit -m "add aur/example-package"
```
