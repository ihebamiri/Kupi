#!/usr/bin/env bash
# Sets Kupi's version everywhere it's recorded: the app itself, the Windows
# file properties and the Linux packages. Used by the release workflow with
# the version from the git tag; run it locally to try a release build.
#
#   scripts/set-version.sh 0.2.0
set -euo pipefail

version="${1:-}"
version="${version#v}"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 <version>, for example 0.2.0 or 0.2.0-rc.1" >&2
  exit 2
fi
# Windows file versions are four numbers; drop a pre-release suffix there.
numeric="${version%%-*}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

sed -i.bak -E "s/^var Version = \".*\"/var Version = \"$version\"/" internal/version/version.go
sed -i.bak -E "s/^(  version: )\".*\"/\1\"$version\"/" build/config.yml
sed -i.bak -E "s/^version: \".*\"/version: \"$version\"/" build/linux/nfpm/nfpm.yaml
sed -i.bak -E \
  -e "s/(\"(file_version|product_version)\": )\"[^\"]*\"/\1\"$numeric\"/" \
  -e "s/(\"(FileVersion|ProductVersion)\": )\"[^\"]*\"/\1\"$version\"/" \
  build/windows/info.json
rm -f internal/version/version.go.bak build/config.yml.bak build/linux/nfpm/nfpm.yaml.bak build/windows/info.json.bak

grep -q "\"$version\"" internal/version/version.go
grep -q "\"$version\"" build/linux/nfpm/nfpm.yaml
echo "Kupi version set to $version"
