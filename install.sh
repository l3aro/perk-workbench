#!/usr/bin/env bash
set -euo pipefail

readonly REPOSITORY="l3aro/perk-workbench"

fail() {
  printf 'install: %s\n' "$1" >&2
  exit 1
}

if [[ -z "${HOME:-}" ]]; then
  fail 'HOME is not set'
fi

if (( $# > 1 )); then
  fail 'usage: install.sh [VERSION]'
fi

version=${1:-${PERK_WORKBENCH_VERSION:-latest}}
install_dir=${PERK_WORKBENCH_INSTALL_DIR:-$HOME/.local/bin}
base_url=${PERK_WORKBENCH_BASE_URL:-https://github.com/$REPOSITORY/releases}
base_url=${base_url%/}
os=${PERK_WORKBENCH_OS:-$(uname -s)}
arch=${PERK_WORKBENCH_ARCH:-$(uname -m)}

case "$version" in
  latest)
    release_path='latest/download'
    ;;
  v[0-9]*|[0-9]*)
    if [[ "$version" == v* ]]; then
      tag=$version
    else
      tag=v$version
    fi
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || \
      fail "invalid version: $version"
    release_path="download/$tag"
    ;;
  *)
    fail "invalid version: $version"
    ;;
esac

case "$os/$arch" in
  Linux/x86_64|Linux/amd64)
    target=linux-amd64
    archive="perk-workbench-$target.tar.gz"
    executable=perk-workbench
    ;;
  Linux/aarch64|Linux/arm64)
    target=linux-arm64
    archive="perk-workbench-$target.tar.gz"
    executable=perk-workbench
    ;;
  Darwin/arm64|Darwin/aarch64)
    target=darwin-arm64
    archive="perk-workbench-$target.tar.gz"
    executable=perk-workbench
    ;;
  MINGW*/x86_64|MSYS*/x86_64|CYGWIN*/x86_64)
    target=windows-amd64
    archive="perk-workbench-$target.zip"
    executable=perk-workbench.exe
    ;;
  *)
    fail "unsupported platform: $os/$arch (supported: Linux amd64/arm64, macOS arm64, Windows amd64)"
    ;;
esac

command -v curl >/dev/null 2>&1 || fail 'curl is required'
command -v install >/dev/null 2>&1 || fail 'install is required'

case "$base_url" in
  https://*)
    curl_options=(--proto '=https' --tlsv1.2)
    ;;
  *)
    # Non-HTTPS URLs are intended only for local testing through PERK_WORKBENCH_BASE_URL.
    curl_options=()
    ;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/perk-workbench-install.XXXXXXXX")
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

archive_path="$work_dir/$archive"
checksum_path="$work_dir/$archive.sha256"
archive_url="$base_url/$release_path/$archive"
checksum_url="$archive_url.sha256"

printf 'Downloading %s for %s/%s...\n' "${version}" "$os" "$arch"
curl --fail --silent --show-error --location --retry 3 "${curl_options[@]}" \
  "$archive_url" --output "$archive_path"
curl --fail --silent --show-error --location --retry 3 "${curl_options[@]}" \
  "$checksum_url" --output "$checksum_path"

expected=$(sed -n '1s/[[:space:]].*$//p' "$checksum_path")
[[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || fail 'release checksum is invalid'

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive_path" | cut -d ' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$archive_path" | cut -d ' ' -f1)
else
  fail 'sha256sum or shasum is required to verify the release'
fi

[[ "$actual" == "$expected" ]] || fail 'release checksum does not match'

extract_dir="$work_dir/extract"
mkdir -p "$extract_dir"
if [[ "$archive" == *.tar.gz ]]; then
  command -v tar >/dev/null 2>&1 || fail 'tar is required for this platform'
  [[ "$(tar -tzf "$archive_path")" == "$executable" ]] || \
    fail 'release archive contains an unexpected file'
  tar -xzf "$archive_path" -C "$extract_dir"
else
  command -v unzip >/dev/null 2>&1 || fail 'unzip is required for Windows archives'
  [[ "$(unzip -Z1 "$archive_path")" == "$executable" ]] || \
    fail 'release archive contains an unexpected file'
  unzip -q "$archive_path" -d "$extract_dir"
fi

source_path="$extract_dir/$executable"
[[ -f "$source_path" && ! -L "$source_path" ]] || fail 'release archive did not contain the executable'

mkdir -p "$install_dir"
temporary_path="$install_dir/.${executable}.tmp.$$"
install -m 0755 "$source_path" "$temporary_path"
mv -f "$temporary_path" "$install_dir/$executable"

printf 'Installed %s to %s\n' "$executable" "$install_dir/$executable"
case ":${PATH:-}:" in
  *:"$install_dir":*) ;;
  *) printf 'Add %s to PATH to run perk-workbench directly.\n' "$install_dir" ;;
esac
