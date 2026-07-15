#!/usr/bin/env bash
set -euo pipefail

SOURCE_APP_NAME="Chrome CDP.app"
INSTALL_DIR="${CHROME_CDP_INSTALL_DIR:-/Applications}"
DEFAULT_ZIP_URL="https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/agent_go/cmd/server/embed_downloads/Chrome-CDP-macOS.zip"
ZIP_URL="${CHROME_CDP_ZIP_URL:-${DEFAULT_ZIP_URL}}"
CDP_PORT="${CHROME_CDP_PORT:-9222}"
OPEN_AFTER_INSTALL="${CHROME_CDP_OPEN_AFTER_INSTALL:-1}"
ADHOC_SIGN="${CHROME_CDP_ADHOC_SIGN:-1}"

usage() {
  cat <<'EOF'
Install a dedicated Chrome CDP launcher on macOS.

Usage: install-chrome-cdp-macOS.sh [--port PORT]

Options:
  --port PORT  CDP port for this launcher (default: 9222)
  -h, --help   Show this help

The default port installs /Applications/Chrome CDP.app and uses
~/.chrome-cdp-profile. Other ports install a separate app such as
/Applications/Chrome CDP 9333.app and use ~/.chrome-cdp-profile-9333.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)
      if [[ $# -lt 2 ]]; then
        echo "--port requires a value." >&2
        exit 2
      fi
      CDP_PORT="$2"
      shift 2
      ;;
    --port=*)
      CDP_PORT="${1#*=}"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! "${CDP_PORT}" =~ ^[0-9]+$ ]]; then
  echo "CDP port must be an integer from 1 to 65535." >&2
  exit 2
fi
CDP_PORT=$((10#${CDP_PORT}))
if (( CDP_PORT < 1 || CDP_PORT > 65535 )); then
  echo "CDP port must be an integer from 1 to 65535." >&2
  exit 2
fi

if [[ "${CDP_PORT}" == "9222" ]]; then
  APP_DISPLAY_NAME="Chrome CDP"
  PROFILE_DIR_NAME=".chrome-cdp-profile"
  BUNDLE_IDENTIFIER="local.chrome.cdp"
else
  APP_DISPLAY_NAME="Chrome CDP ${CDP_PORT}"
  PROFILE_DIR_NAME=".chrome-cdp-profile-${CDP_PORT}"
  BUNDLE_IDENTIFIER="local.chrome.cdp.port${CDP_PORT}"
fi
APP_NAME="${APP_DISPLAY_NAME}.app"
APP_PATH="${INSTALL_DIR}/${APP_NAME}"

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

SOURCE_APP_PATH="${tmp_dir}/${SOURCE_APP_NAME}"
if [[ ! -d "${SOURCE_APP_PATH}" ]]; then
  echo "Downloaded archive did not contain ${SOURCE_APP_NAME}." >&2
  exit 1
fi

LAUNCHER_PATH="${SOURCE_APP_PATH}/Contents/MacOS/Chrome CDP"
INFO_PLIST_PATH="${SOURCE_APP_PATH}/Contents/Info.plist"
if [[ ! -f "${LAUNCHER_PATH}" || ! -f "${INFO_PLIST_PATH}" ]]; then
  echo "Downloaded ${SOURCE_APP_NAME} is missing its launcher or Info.plist." >&2
  exit 1
fi

# The downloadable app is a template for port 9222. Customize the installed
# copy so every port has its own app identity, debugging port, and Chrome
# profile. This allows multiple login identities to run at the same time.
sed -i '' \
  -e "s|^# Launch Chrome with CDP.*|# Launch Chrome with CDP (remote debugging) on port ${CDP_PORT}|" \
  -e "s|^USER_DATA_DIR=.*|USER_DATA_DIR=\"\${HOME}/${PROFILE_DIR_NAME}\"|" \
  -e "s|^PORT=.*|PORT=${CDP_PORT}|" \
  "${LAUNCHER_PATH}"
plutil -replace CFBundleDisplayName -string "${APP_DISPLAY_NAME}" "${INFO_PLIST_PATH}"
plutil -replace CFBundleName -string "${APP_DISPLAY_NAME}" "${INFO_PLIST_PATH}"
plutil -replace CFBundleIdentifier -string "${BUNDLE_IDENTIFIER}" "${INFO_PLIST_PATH}"

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
run_install_command ditto "${SOURCE_APP_PATH}" "${APP_PATH}"
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
  echo "Opening ${APP_NAME} on port ${CDP_PORT}"
  if ! open "${APP_PATH}"; then
    echo "macOS blocked the first launch. Open System Settings > Privacy & Security and allow ${APP_DISPLAY_NAME}, then run: open '${APP_PATH}'" >&2
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
  echo "If macOS showed a prompt, approve it and open ${APP_DISPLAY_NAME} again from Spotlight/Launchpad." >&2
  exit 1
fi

echo "Open it from Spotlight/Launchpad, or run: open '${APP_PATH}'"
