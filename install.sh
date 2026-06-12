#!/usr/bin/env bash
# Runloop installer — downloads the latest macOS dmg from GitHub Releases,
# installs Runloop.app to /Applications, and strips the quarantine flag so
# Gatekeeper doesn't show "Runloop is damaged".
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/install.sh | bash
#
# Override the version with RUNLOOP_VERSION (e.g. RUNLOOP_VERSION=v1.25.6 …).

set -euo pipefail

REPO="manishiitg/mcp-agent-builder-go"
APP_NAME="Runloop"
INSTALL_DIR="/Applications"

log()  { printf '\033[1;34m[runloop]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[runloop]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[runloop]\033[0m %s\n' "$*" >&2; exit 1; }

# ---- Preflight --------------------------------------------------------------

[ "$(uname -s)" = "Darwin" ] || die "This installer is macOS-only."

ARCH="$(uname -m)"
case "$ARCH" in
  arm64) ;; # Apple Silicon — the only build we currently ship
  x86_64) die "Intel Macs are not currently supported (arm64 build only). Build from source: https://github.com/${REPO}/tree/main/desktop" ;;
  *)     die "Unsupported architecture: $ARCH" ;;
esac

for cmd in curl hdiutil xattr; do
  command -v "$cmd" >/dev/null 2>&1 || die "Required command '$cmd' not found in PATH."
done

ensure_tmux() {
  if command -v tmux >/dev/null 2>&1; then
    local version major
    version="$(tmux -V 2>/dev/null || true)"
    major="$(printf '%s\n' "$version" | sed -E 's/^tmux ([0-9]+).*/\1/')"
    if [ "$major" -ge 3 ] 2>/dev/null; then
      log "Claude Code experimental runtime dependency found: ${version}"
      return 0
    fi
    warn "Claude Code experimental runtime dependency ${version:-unknown} found, but version 3.x or newer is required."
  fi

  if command -v brew >/dev/null 2>&1; then
    log "Installing/upgrading Claude Code experimental runtime dependency with Homebrew…"
    brew upgrade tmux || brew install tmux || warn "Install failed. Claude Code experimental mode will not work until you install tmux 3.x or newer: brew install tmux"
    return 0
  fi

  warn "Claude Code experimental mode requires tmux 3.x or newer. Install Homebrew, then run: brew install tmux"
}

ensure_tmux

ensure_go_for_mcpbridge() {
  if command -v go >/dev/null 2>&1; then
    return 0
  fi

  if command -v brew >/dev/null 2>&1; then
    log "Go is required to install mcpbridge for this dmg; installing Go with Homebrew..."
    if brew install go; then
      return 0
    fi
    warn "Homebrew could not install Go. Claude Code/Codex/Gemini CLI tool access may fail until Go and mcpbridge are installed."
    return 1
  fi

  warn "Go is not installed, so the installer cannot build mcpbridge for this dmg."
  warn "Install Go from https://go.dev/dl/ or Homebrew, then run: GOBIN=\"\$HOME/go/bin\" go install github.com/manishiitg/mcpagent/cmd/mcpbridge@latest"
  return 1
}

ensure_mcpbridge() {
  local home_bridge="${HOME}/go/bin/mcpbridge"

  if command -v mcpbridge >/dev/null 2>&1; then
    log "MCP bridge found: $(command -v mcpbridge)"
    return 0
  fi

  if [ -x "$home_bridge" ]; then
    log "MCP bridge found: ${home_bridge}"
    return 0
  fi

  if ! ensure_go_for_mcpbridge; then
    return 0
  fi

  log "Installing MCP bridge for CLI provider tool access..."
  mkdir -p "${HOME}/go/bin"
  if GOBIN="${HOME}/go/bin" GOWORK=off go install github.com/manishiitg/mcpagent/cmd/mcpbridge@latest; then
    log "MCP bridge installed: ${home_bridge}"
    return 0
  fi
  warn "Failed to install mcpbridge. Claude Code/Codex/Gemini CLI tool access may fail until it is installed."
}

# ---- Resolve version --------------------------------------------------------

VERSION="${RUNLOOP_VERSION:-}"
if [ -z "$VERSION" ]; then
  log "Looking up the latest release…"
  # Use the redirect from /releases/latest to find the tag — avoids needing jq
  # and works without a GitHub token (rate-limited but fine for installs).
  VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" \
              | sed -E 's|.*/tag/||')"
  [ -n "$VERSION" ] || die "Could not determine latest release."
fi
log "Installing version $VERSION"

# ---- Quit running app -------------------------------------------------------

if pgrep -fq "${APP_NAME}.app/Contents/MacOS"; then
  log "Quitting running ${APP_NAME}…"
  osascript -e "tell application \"${APP_NAME}\" to quit" 2>/dev/null || true
  sleep 1
  # Force-kill anything still alive (helpers, leftover servers)
  pkill -f "${APP_NAME}.app" 2>/dev/null || true
fi

# ---- Download dmg -----------------------------------------------------------

VERSION_NO_V="${VERSION#v}"
DMG_NAME="${APP_NAME}-${VERSION_NO_V}-arm64.dmg"
DMG_URL="https://github.com/${REPO}/releases/download/${VERSION}/${DMG_NAME}"

TMP_DIR="$(mktemp -d -t runloop-install)"
trap 'rm -rf "$TMP_DIR"' EXIT
DMG_PATH="${TMP_DIR}/${DMG_NAME}"

log "Downloading ${DMG_NAME} (~155 MB)…"
if ! curl -fL --progress-bar -o "$DMG_PATH" "$DMG_URL"; then
  die "Download failed. Check that ${VERSION} has an arm64 dmg asset: https://github.com/${REPO}/releases/tag/${VERSION}"
fi

# ---- Mount + copy app -------------------------------------------------------

log "Mounting dmg…"
MOUNT_POINT="$(hdiutil attach -nobrowse -noverify -noautoopen "$DMG_PATH" \
              | tail -1 | awk '{ $1=""; $2=""; sub(/^  */,""); print }')"
[ -d "$MOUNT_POINT" ] || die "Failed to locate mount point after attach."

# Make sure we always detach
detach_mount() { hdiutil detach -quiet "$MOUNT_POINT" 2>/dev/null || true; }
trap 'detach_mount; rm -rf "$TMP_DIR"' EXIT

SOURCE_APP="${MOUNT_POINT}/${APP_NAME}.app"
[ -d "$SOURCE_APP" ] || die "Could not find ${APP_NAME}.app inside the dmg."

DEST_APP="${INSTALL_DIR}/${APP_NAME}.app"
if [ -e "$DEST_APP" ]; then
  log "Removing existing ${DEST_APP}…"
  rm -rf "$DEST_APP" || die "Could not remove existing app. Try: sudo rm -rf '$DEST_APP'"
fi

log "Copying ${APP_NAME}.app to ${INSTALL_DIR}…"
cp -R "$SOURCE_APP" "$DEST_APP" || die "Copy failed. Make sure $INSTALL_DIR is writable, or run with sudo."

ensure_mcpbridge

detach_mount

# ---- Strip quarantine -------------------------------------------------------

log "Clearing quarantine attribute…"
# Older macOS `xattr` doesn't support -r. Walk the bundle ourselves.
# We delete only the quarantine xattr to avoid wiping any signing-related ones.
if ! find "$DEST_APP" -exec xattr -d com.apple.quarantine {} \; 2>/dev/null; then
  warn "xattr cleanup had errors; if you see a 'damaged' warning on launch, run:  xattr -cr '$DEST_APP'"
fi

# ---- Done -------------------------------------------------------------------

log "Installed ${APP_NAME} ${VERSION} to ${DEST_APP}"
log "Launching ${APP_NAME}…"
open "$DEST_APP"
