#!/usr/bin/env bash
set -euo pipefail

GOOS=${1:?GOOS is required}
GOARCH=${2:?GOARCH is required}
DIST=${3:?output directory is required}
BINARY=${4:?built binary is required}

case "$GOOS/$GOARCH" in
  darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64) ;;
  *) echo "unsupported target: $GOOS/$GOARCH" >&2; exit 1 ;;
esac

if [[ ! -f "$BINARY" || ! -s "$BINARY" ]]; then
  echo "missing or empty built binary: $BINARY" >&2
  exit 1
fi

if [[ "$GOOS" != windows && ! -x "$BINARY" ]]; then
  echo "built binary is not executable: $BINARY" >&2
  exit 1
fi

mkdir -p "$DIST"
TARGET="${GOOS}-${GOARCH}"
EXECUTABLE="perk-workbench"
[[ "$GOOS" == windows ]] && EXECUTABLE+=".exe"
if [[ "$GOOS" == windows ]]; then
  ASSET="perk-workbench-${TARGET}.zip"
else
  ASSET="perk-workbench-${TARGET}.tar.gz"
fi

EPOCH=${SOURCE_DATE_EPOCH:-$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." log -1 --format=%ct)}
[[ "$EPOCH" =~ ^[0-9]+$ ]] || { echo "invalid SOURCE_DATE_EPOCH: $EPOCH" >&2; exit 1; }

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
    return
  fi
  local python_bin=python3
  command -v "$python_bin" >/dev/null 2>&1 || python_bin=python
  "$python_bin" -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$1"
}

python_bin=python3
command -v "$python_bin" >/dev/null 2>&1 || python_bin=python
"$python_bin" - "$BINARY" "$DIST/$ASSET" "$EPOCH" "$EXECUTABLE" "$GOOS" <<'PY'
import sys
import tarfile
import time
import zipfile
import gzip

binary, output, epoch, executable, goos = sys.argv[1:]
epoch = int(epoch)
if goos == "windows":
    info = zipfile.ZipInfo(executable, time.gmtime(max(epoch, 315532800))[:6])
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = 0o755 << 16
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        with open(binary, "rb") as source:
            archive.writestr(info, source.read())
else:
    with open(output, "wb") as raw:
        with gzip.GzipFile(fileobj=raw, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                info = archive.gettarinfo(binary, arcname=executable)
                info.uid = 0
                info.gid = 0
                info.uname = "root"
                info.gname = "root"
                info.mtime = epoch
                info.mode = 0o755
                info.pax_headers = {}
                with open(binary, "rb") as source:
                    archive.addfile(info, source)
PY

ARCHIVE_SHA=$(hash_file "$DIST/$ASSET")
printf '%s  %s\n' "$ARCHIVE_SHA" "$ASSET" > "$DIST/$ASSET.sha256"
printf '%s %s %s %s\n' perk-workbench "$TARGET" "$ASSET" "$ARCHIVE_SHA"
