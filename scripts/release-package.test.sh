#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

binary="$TMP/input"
printf 'self-hosting binary\n' > "$binary"
chmod 755 "$binary"

"$ROOT/scripts/release-package.sh" linux amd64 "$TMP/linux" "$binary" >/dev/null
python3 - "$TMP/linux/perk-workbench-linux-amd64.tar.gz" "$TMP/linux/perk-workbench-linux-amd64.tar.gz.sha256" <<'PY'
import hashlib
import sys
import tarfile

archive, checksum = sys.argv[1:]
with open(archive, "rb") as source:
    digest = hashlib.sha256(source.read()).hexdigest()
with open(checksum, encoding="utf-8") as source:
    expected, name = source.read().split()
assert expected == digest
assert name == "perk-workbench-linux-amd64.tar.gz"
with tarfile.open(archive, "r:gz") as package:
    names = package.getnames()
    assert names == ["perk-workbench"], names
PY

"$ROOT/scripts/release-package.sh" windows amd64 "$TMP/windows" "$binary" >/dev/null
python3 - "$TMP/windows/perk-workbench-windows-amd64.zip" "$TMP/windows/perk-workbench-windows-amd64.zip.sha256" <<'PY'
import hashlib
import sys
import zipfile

archive, checksum = sys.argv[1:]
with open(archive, "rb") as source:
    digest = hashlib.sha256(source.read()).hexdigest()
with open(checksum, encoding="utf-8") as source:
    expected, name = source.read().split()
assert expected == digest
assert name == "perk-workbench-windows-amd64.zip"
with zipfile.ZipFile(archive) as package:
    assert package.namelist() == ["perk-workbench.exe"]
PY

if "$ROOT/scripts/release-package.sh" linux 386 "$TMP/invalid" "$binary" >/dev/null 2>&1; then
  echo "unsupported target unexpectedly packaged" >&2
  exit 1
fi

echo "release package tests passed"
