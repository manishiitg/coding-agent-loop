#!/usr/bin/env bash
set -euo pipefail

APP_NAME="Chrome CDP.app"
INSTALL_DIR="${CHROME_CDP_INSTALL_DIR:-/Applications}"
APP_PATH="${INSTALL_DIR}/${APP_NAME}"
DEFAULT_ZIP_URL="https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/agent_go/cmd/server/embed_downloads/Chrome-CDP-macOS.zip"
ZIP_URL="${CHROME_CDP_ZIP_URL:-${DEFAULT_ZIP_URL}}"
CDP_PORT="${CHROME_CDP_PORT:-9222}"
OPEN_AFTER_INSTALL="${CHROME_CDP_OPEN_AFTER_INSTALL:-1}"
ADHOC_SIGN="${CHROME_CDP_ADHOC_SIGN:-1}"

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

run_install_command() {
  if [[ -w "${INSTALL_DIR}" ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

echo "Installing ${APP_NAME} to ${INSTALL_DIR}"
if [[ -e "${APP_PATH}" ]]; then
  run_install_command rm -rf "${APP_PATH}"
fi
run_install_command ditto "${tmp_dir}/${APP_NAME}" "${APP_PATH}"
run_install_command chmod +x "${APP_PATH}/Contents/MacOS/Chrome CDP"

# Clear quarantine/extended attributes so macOS does not show the misleading
# "damaged" warning for this locally installed helper app.
if command -v xattr >/dev/null 2>&1; then
  run_install_command find "${APP_PATH}" -exec xattr -c {} + >/dev/null 2>&1 || true
fi

if [[ "${ADHOC_SIGN}" != "0" ]] && command -v codesign >/dev/null 2>&1; then
  echo "Applying a local ad-hoc signature"
  if ! run_install_command codesign --force --deep --sign - "${APP_PATH}" >/dev/null 2>&1; then
    echo "Ad-hoc signing was skipped; macOS may ask you to approve the app on first launch." >&2
  fi
fi

echo "Installed ${APP_PATH}"

if [[ "${OPEN_AFTER_INSTALL}" != "0" ]]; then
  echo "Opening ${APP_NAME}"
  if ! open "${APP_PATH}"; then
    echo "macOS blocked the first launch. Open System Settings > Privacy & Security and allow Chrome CDP, then run: open -a 'Chrome CDP'" >&2
    exit 1
  fi

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
      echo "Chrome CDP is reachable on http://127.0.0.1:${CDP_PORT}"
      exit 0
    fi
    sleep 1
  done

  echo "Chrome CDP was opened, but port ${CDP_PORT} is not reachable yet." >&2
  echo "If macOS showed a prompt, approve it and open Chrome CDP again from Spotlight/Launchpad." >&2
  exit 1
fi

echo "Open it from Spotlight/Launchpad, or run: open -a 'Chrome CDP'"
