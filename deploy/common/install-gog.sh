#!/usr/bin/env bash
# Install or update gog (github.com/openclaw/gogcli), the Gmail connector's
# host CLI, to the LATEST release, the same "always latest" policy as
# agent-browser. The download is verified against the release's own
# checksums.txt. Usage: install-gog.sh <prefix>  (installs <prefix>/bin/gog)
#
# Runs on the target box (piped over SSH or called by an on-box deploy). If
# GitHub is unreachable an existing gog is kept (with a warning); a box with
# no gog at all fails, because Gmail cannot work without it.
set -euo pipefail

prefix="${1:?usage: install-gog.sh <prefix>}"
bin_dir="$prefix/bin"
target="$bin_dir/gog"
repo="openclaw/gogcli"

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "install-gog: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

current=""
[[ -x "$target" ]] && current="$("$target" --version 2>/dev/null | awk '{print $1}' | sed 's/^v//')" || true

fail_or_keep() {
  if [[ -n "$current" ]]; then
    echo "WARNING: install-gog: $1; keeping gog $current" >&2
    exit 0
  fi
  echo "FATAL: install-gog: $1 and no gog is installed" >&2
  exit 1
}

tag="$(curl -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$repo/releases/latest" 2>/dev/null \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])' 2>/dev/null)" \
  || fail_or_keep "could not resolve the latest release"
version="${tag#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail_or_keep "unexpected release tag '$tag'"

if [[ "$current" == "$version" ]]; then
  echo "    gog: $version (latest)"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
archive="gogcli_${version}_linux_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
curl -fsSL --proto '=https' --tlsv1.2 "$base/$archive" -o "$tmp/$archive" || fail_or_keep "download of $archive failed"
curl -fsSL --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$tmp/checksums.txt" || fail_or_keep "download of checksums.txt failed"
expected="$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")"
[[ -n "$expected" ]] || fail_or_keep "$archive is not listed in checksums.txt"
echo "$expected  $tmp/$archive" | sha256sum -c - >/dev/null || fail_or_keep "checksum mismatch for $archive"
tar -xzf "$tmp/$archive" -C "$tmp" gog 2>/dev/null || tar -xzf "$tmp/$archive" -C "$tmp" ./gog
install -d -m 0755 "$bin_dir"
install -m 0755 "$tmp/gog" "$target.new"
mv -f "$target.new" "$target"
echo "    gog: ${current:-none} -> $("$target" --version 2>/dev/null | awk '{print $1}')"
