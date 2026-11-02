#!/usr/bin/env bash
set -euo pipefail

# Run from the repository root. Cross-compilation requires no target SDK or CGO.
goos=${1:?usage: build-release.sh GOOS GOARCH [OUTPUT_DIR]}
goarch=${2:?usage: build-release.sh GOOS GOARCH [OUTPUT_DIR]}
out=${3:-dist}
case "$goos/$goarch" in
  linux/amd64|linux/arm64|windows/amd64|windows/arm64|darwin/amd64|darwin/arm64) ;;
  *) echo 'unsupported release target' >&2; exit 1 ;;
esac
version=$(tr -d '\r\n' < VERSION)
[[ "$version" == '1.0.0' ]] || { echo 'expected release version 1.0.0' >&2; exit 1; }
mkdir -p "$out"
work=$(mktemp -d "$out/.package.XXXXXX")
trap 'rm -rf "$work"' EXIT
binary=sf-typings
[[ "$goos" != windows ]] || binary=sf-typings.exe
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$work/$binary" .
cp LICENSE NOTICE "$work/"
name="sf-typings_${version}_${goos}_${goarch}"
if [[ "$goos" == windows ]]; then
  # Python is available on the Ubuntu release runner; ZIP also works from Windows.
  python - "$work" "$out/$name.zip" <<'PY'
import pathlib
import sys
import zipfile
with zipfile.ZipFile(sys.argv[2], 'w', zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(pathlib.Path(sys.argv[1]).iterdir()):
        archive.write(path, path.name)
PY
else
  tar -czf "$out/$name.tar.gz" -C "$work" "$binary" LICENSE NOTICE
fi
