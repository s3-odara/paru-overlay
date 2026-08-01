# paru-overlay

s3-odara's AUR overlay

## paru.conf

```ini
Include = /etc/paru.conf

[options]
Mode = rp

[paru-overlay]
Url = https://github.com/s3-odara/paru-overlay
Depth = 3
```

## Adding PKGBUILD

```sh
pkgbase=example-package
tmp=$(mktemp -d)
git clone "https://aur.archlinux.org/${pkgbase}.git" "$tmp/$pkgbase"
mkdir -p "packages/$pkgbase"
find "packages/$pkgbase" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -a "$tmp/$pkgbase/." "packages/$pkgbase/"
rm -rf "packages/$pkgbase/.git"
rm -rf "$tmp"
git add "packages/$pkgbase"
git commit -m "add pkg: $pkgbase"
```

or locally

```sh
mkdir -p packages/example-package
cp PKGBUILD *.install *.patch packages/example-package/ 2>/dev/null || true
(
  cd packages/example-package
  makepkg --printsrcinfo > .SRCINFO
)
git add packages/example-package
git commit -m "add pkg: example-package"
```
