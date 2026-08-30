#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

binary="$TMP/input"
write_binary() {
  printf '#!/usr/bin/env bash\nprintf "%s\\n"\n' "$1" > "$binary"
  chmod 755 "$binary"
}

release_dir="$TMP/releases/download/v1.0.0"
mkdir -p "$release_dir"
write_binary 'perk-workbench v1'
"$ROOT/scripts/release-package.sh" linux amd64 "$release_dir" "$binary" >/dev/null

base_url="file://$TMP/releases"
install_dir="$TMP/install"
PERK_WORKBENCH_BASE_URL="$base_url" \
PERK_WORKBENCH_INSTALL_DIR="$install_dir" \
PERK_WORKBENCH_OS=Linux \
PERK_WORKBENCH_ARCH=x86_64 \
  "$ROOT/install.sh" 1.0.0 >/dev/null

[[ -x "$install_dir/perk-workbench" ]]
[[ "$("$install_dir/perk-workbench")" == 'perk-workbench v1' ]]

write_binary 'perk-workbench v2'
"$ROOT/scripts/release-package.sh" linux amd64 "$release_dir" "$binary" >/dev/null
PERK_WORKBENCH_BASE_URL="$base_url" \
PERK_WORKBENCH_INSTALL_DIR="$install_dir" \
PERK_WORKBENCH_OS=Linux \
PERK_WORKBENCH_ARCH=x86_64 \
  "$ROOT/install.sh" 1.0.0 >/dev/null
[[ "$("$install_dir/perk-workbench")" == 'perk-workbench v2' ]]

latest_dir="$TMP/releases/latest/download"
mkdir -p "$latest_dir"
cp "$release_dir/perk-workbench-linux-amd64.tar.gz" "$latest_dir/"
cp "$release_dir/perk-workbench-linux-amd64.tar.gz.sha256" "$latest_dir/"
PERK_WORKBENCH_BASE_URL="$base_url" \
PERK_WORKBENCH_INSTALL_DIR="$TMP/latest-install" \
PERK_WORKBENCH_OS=Linux \
PERK_WORKBENCH_ARCH=x86_64 \
  "$ROOT/install.sh" >/dev/null
[[ "$("$TMP/latest-install/perk-workbench")" == 'perk-workbench v2' ]]

printf '%064d  %s\n' 0 'perk-workbench-linux-amd64.tar.gz' > \
  "$release_dir/perk-workbench-linux-amd64.tar.gz.sha256"
if PERK_WORKBENCH_BASE_URL="$base_url" \
  PERK_WORKBENCH_INSTALL_DIR="$install_dir" \
  PERK_WORKBENCH_OS=Linux \
  PERK_WORKBENCH_ARCH=x86_64 \
  "$ROOT/install.sh" 1.0.0 >/dev/null 2>&1; then
  echo 'invalid checksum unexpectedly installed' >&2
  exit 1
fi
[[ "$("$install_dir/perk-workbench")" == 'perk-workbench v2' ]]

"$ROOT/scripts/release-package.sh" windows amd64 "$release_dir" "$binary" >/dev/null
PERK_WORKBENCH_BASE_URL="$base_url" \
PERK_WORKBENCH_INSTALL_DIR="$TMP/windows-install" \
PERK_WORKBENCH_OS=MINGW64_NT-10.0 \
PERK_WORKBENCH_ARCH=x86_64 \
  "$ROOT/install.sh" 1.0.0 >/dev/null
[[ -x "$TMP/windows-install/perk-workbench.exe" ]]
[[ "$("$TMP/windows-install/perk-workbench.exe")" == 'perk-workbench v2' ]]

if PERK_WORKBENCH_OS=Linux PERK_WORKBENCH_ARCH=386 \
  PERK_WORKBENCH_BASE_URL="$base_url" \
  PERK_WORKBENCH_INSTALL_DIR="$TMP/invalid" \
  "$ROOT/install.sh" 1.0.0 >/dev/null 2>&1; then
  echo 'unsupported target unexpectedly installed' >&2
  exit 1
fi

echo 'install script tests passed'
