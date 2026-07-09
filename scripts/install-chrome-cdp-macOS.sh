#!/usr/bin/env bash
set -euo pipefail

APP_NAME="Chrome CDP.app"
INSTALL_DIR="${CHROME_CDP_INSTALL_DIR:-/Applications}"
APP_PATH="${INSTALL_DIR}/${APP_NAME}"
DEFAULT_ZIP_URL="https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/agent_go/cmd/server/embed_downloads/Chrome-CDP-macOS.zip"
ZIP_URL="${CHROME_CDP_ZIP_URL:-${DEFAULT_ZIP_URL}}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "Chrome CDP.app installer is only for macOS." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required but was not found." >&2
  exit 1
fi
if ! command -v unzip >/dev/null 2>&1; then
  echo "unzip is required but was not found." >&2
  exit 1
fi
if ! command -v ditto >/dev/null 2>&1; then
  echo "ditto is required but was not found." >&2
  exit 1
fi

if [[ ! -x "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" && ! -x "/Applications/Chromium.app/Contents/MacOS/Chromium" ]]; then
  echo "Google Chrome or Chromium must be installed in /Applications before installing Chrome CDP.app." >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

echo "Downloading Chrome CDP.app from ${ZIP_URL}"
curl -fsSL "${ZIP_URL}" -o "${tmp_dir}/Chrome-CDP-macOS.zip"
unzip -q "${tmp_dir}/Chrome-CDP-macOS.zip" -d "${tmp_dir}"

if [[ ! -d "${tmp_dir}/${APP_NAME}" ]]; then
  echo "Downloaded archive did not contain ${APP_NAME}." >&2
  exit 1
fi

sudo_cmd=()
if [[ ! -w "${INSTALL_DIR}" ]]; then
  sudo_cmd=(sudo)
fi

echo "Installing ${APP_NAME} to ${INSTALL_DIR}"
if [[ -e "${APP_PATH}" ]]; then
  "${sudo_cmd[@]}" rm -rf "${APP_PATH}"
fi
"${sudo_cmd[@]}" ditto "${tmp_dir}/${APP_NAME}" "${APP_PATH}"
"${sudo_cmd[@]}" chmod +x "${APP_PATH}/Contents/MacOS/Chrome CDP"

# Clear quarantine/extended attributes so macOS does not show the misleading
# "damaged" warning for this locally installed helper app.
if command -v xattr >/dev/null 2>&1; then
  "${sudo_cmd[@]}" xattr -dr com.apple.quarantine "${APP_PATH}" 2>/dev/null || true
  "${sudo_cmd[@]}" xattr -c "${APP_PATH}" 2>/dev/null || true
fi

echo "Installed ${APP_PATH}"
echo "Open it from Spotlight/Launchpad, or run: open -a 'Chrome CDP'"
