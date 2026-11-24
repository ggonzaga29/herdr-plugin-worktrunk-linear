#!/bin/sh
# Maintainer: scripts/release.sh 0.2.0 — sets the manifest version, builds the menu for every
# platform, commits checksums.txt, tags v0.2.0, pushes, and uploads the binaries as a GitHub release.
set -eu
cd "$(dirname "$0")/.."
v=${1:?usage: scripts/release.sh VERSION (e.g. 0.2.0)}
case $v in v*) v=${v#v} ;; esac

[ "$(git branch --show-current)" = main ] || { echo "release from main" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "commit or stash your changes first" >&2; exit 1; }
git rev-parse -q --verify "refs/tags/v$v" >/dev/null && { echo "v$v exists" >&2; exit 1; }
go vet ./... && go test ./...

sed -i.bak "s/^version *= *\".*\"/version = \"$v\"/" herdr-plugin.toml && rm herdr-plugin.toml.bak
rm -rf dist && mkdir dist
for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 \
    go build -trimpath -ldflags='-s -w' -o "dist/herdr-wtl-${target%/*}-${target#*/}" .
done
{ echo "# version $v"; (cd dist && shasum -a 256 herdr-wtl-*); } >checksums.txt

git add herdr-plugin.toml checksums.txt
git commit -m "Release v$v"
git tag "v$v"
git push origin main "v$v"
gh release create "v$v" dist/herdr-wtl-* --title "v$v" --notes "Prebuilt menu binaries for \`herdr plugin install\`; checksums are in checksums.txt."
