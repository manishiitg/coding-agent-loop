#!/usr/bin/env bash
# Backend dependency only. No slack login, credentials, shell-profile edits, or latest-release lookup.
set -euo pipefail
SLACK_CLI_VERSION="4.8.0"
install_prefix="${1:-/usr/local}"
[[ "$install_prefix" == /* ]] || { echo 'Slack install prefix must be absolute' >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || { echo 'This pinned installer supports Linux servers only' >&2; exit 1; }
case "$(uname -m)" in
  x86_64|amd64) arch=amd64; archive_sha256=533ebc242561a79c6aaf238c3417ce113d1257ace80cf90f1e5f852d8ec9ca7b ;;
  aarch64|arm64) arch=arm64; archive_sha256=dbfc62385ac35d66aa3356d2a99ee1444bd05eafb7b14ef08235e408a2fae6cb ;;
  *) echo 'Unsupported Slack CLI server architecture' >&2; exit 1 ;;
esac
binary="$install_prefix/bin/slack"
if [[ -x "$binary" ]] && [[ "$("$binary" --skip-update version)" == "Using slack v$SLACK_CLI_VERSION" ]]; then
  echo "Slack CLI $SLACK_CLI_VERSION already installed at $binary"
  exit 0
fi
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
archive="slack_cli_${SLACK_CLI_VERSION}_linux_${arch}.tar.gz"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
  "https://downloads.slack-edge.com/slack-cli/$archive" -o "$stage/$archive"
printf '%s  %s\n' "$archive_sha256" "$stage/$archive" | sha256sum -c -
tar -xzf "$stage/$archive" -C "$stage" bin/slack
[[ "$("$stage/bin/slack" --skip-update version)" == "Using slack v$SLACK_CLI_VERSION" ]] || { echo 'Slack CLI version mismatch' >&2; exit 1; }
install -d -m 0755 "$install_prefix/bin"
install -m 0755 "$stage/bin/slack" "$install_prefix/bin/.slack-${SLACK_CLI_VERSION}-new"
mv -f "$install_prefix/bin/.slack-${SLACK_CLI_VERSION}-new" "$binary"
"$binary" --skip-update version
