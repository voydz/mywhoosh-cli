#!/usr/bin/env bash
# Package the native standalone binary with its license and attribution.
set -euo pipefail
version="${1:?Usage: package.sh VERSION}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf 'Expected a stable semantic version, got %s\n' "$version" >&2
  exit 2
fi
os="$(go env GOOS)"
arch="$(go env GOARCH)"
case "$os/$arch" in
  darwin/arm64) platform="macos-arm64" ;;
  darwin/amd64) platform="macos-x86_64" ;;
  linux/amd64) platform="linux-x86_64" ;;
  linux/arm64) platform="linux-aarch64" ;;
  *) printf 'Unsupported release target: %s/%s\n' "$os" "$arch" >&2; exit 2 ;;
esac
archive="dist/mywhoosh-cli-${version}-${platform}.tar.gz"
tar -czf "$archive" -C dist mywhoosh -C .. LICENSE THIRD_PARTY_NOTICES.md
python3 - "$archive" <<'PY'
import hashlib
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
digest = hashlib.sha256(path.read_bytes()).hexdigest()
path.with_name(path.name + '.sha256').write_text(digest + '\n')
print(f'{digest}  {path.name}')
PY
