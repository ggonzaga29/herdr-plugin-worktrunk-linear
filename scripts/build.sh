#!/bin/sh
# Install step (`herdr plugin install` runs it from the manifest): puts the menu binary at
# build/herdr-wtl. Uses the prebuilt binary from this version's GitHub release, checked against
# checksums.txt; builds with Go only when no prebuilt binary fits.
set -eu
cd "$(dirname "$0")/.."

repo=ggonzaga29/herdr-plugin-worktrunk-linear
version=$(sed -n 's/^version *= *"\(.*\)"/\1/p' herdr-plugin.toml | head -1)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) arch=$(uname -m) ;;
esac
asset=herdr-wtl-$os-$arch
mkdir -p build

sha256() { if command -v shasum >/dev/null; then shasum -a 256 "$1"; else sha256sum "$1"; fi | cut -d' ' -f1; }

prebuilt() {
  [ "$(sed -n 's/^# version //p' checksums.txt 2>/dev/null)" = "$version" ] || return 1
  want=$(awk -v a="$asset" '$2 == a { print $1 }' checksums.txt)
  [ -n "$want" ] || return 1
  command -v curl >/dev/null || return 1
  tmp=build/herdr-wtl.download
  curl -fsSL --retry 2 -o "$tmp" "https://github.com/$repo/releases/download/v$version/$asset" || return 1
  if [ "$(sha256 "$tmp")" != "$want" ]; then
    echo "herdr-wtl: $asset from release v$version does not match checksums.txt; not using it" >&2
    rm -f "$tmp"
    return 1
  fi
  chmod +x "$tmp" && mv "$tmp" build/herdr-wtl
}

if prebuilt; then
  echo "herdr-wtl: installed prebuilt $asset v$version"
elif command -v go >/dev/null; then
  echo "herdr-wtl: no prebuilt $asset for v$version; building with $(go version)"
  CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/herdr-wtl .
else
  echo "herdr-wtl: no prebuilt $asset for v$version and Go is not installed (https://go.dev/dl)" >&2
  exit 1
fi

# Runtime tools: the menu names any that are missing, but say it here too.
for t in git jq wt linear claude; do
  command -v "$t" >/dev/null || echo "herdr-wtl: note: $t not found on PATH (see the README's Requirements)" >&2
done
exit 0
