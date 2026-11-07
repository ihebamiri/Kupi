#!/usr/bin/env bash
# Gathers the Linux build outputs into one folder with consistent names:
# a tarball of the binary, the AppImage, and the .deb and .rpm packages.
#
#   scripts/collect-linux-packages.sh 0.2.0 dist
set -euo pipefail

version="${1:?usage: $0 <version> <out-dir>}"
version="${version#v}"
out="${2:?usage: $0 <version> <out-dir>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mkdir -p "$out"

find_one() {
  local found
  found="$(find bin build/linux -maxdepth 3 -type f -name "$1" 2>/dev/null | head -n 1)"
  if [ -z "$found" ]; then
    echo "missing: $1" >&2
    exit 1
  fi
  echo "$found"
}

# The plain binary, with the license and the user guide.
stage="$(mktemp -d)/kupi-$version-linux-amd64"
mkdir -p "$stage/docs"
cp bin/kupi LICENSE NOTICE "$stage/"
cp docs/USER_GUIDE.md "$stage/docs/"
[ -f docs/TESTING-LINUX.md ] && cp docs/TESTING-LINUX.md "$stage/docs/"
cp -r docs/testdata "$stage/docs/testdata"
tar -C "$(dirname "$stage")" -czf "$out/kupi-$version-linux-amd64.tar.gz" "$(basename "$stage")"

cp "$(find_one '*.AppImage')" "$out/kupi-$version-linux-x86_64.AppImage"
chmod +x "$out/kupi-$version-linux-x86_64.AppImage"
cp "$(find_one '*.deb')" "$out/kupi-$version-linux-amd64.deb"
cp "$(find_one '*.rpm')" "$out/kupi-$version-linux-x86_64.rpm"

ls -l "$out"
