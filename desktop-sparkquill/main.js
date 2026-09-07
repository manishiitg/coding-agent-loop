// SparkQuill desktop shell.
//
// Spawns the same two platform binaries the AgentWorks desktop spawns
// (workspace-server + agent-server) and serves the main frontend build as
// the agent server's static/ directory, pinned to the SparkQuill product
// surface through the runtime config env (lib/agentEnv.js). The turn engine, tools, and
// workspace API are shared with AgentWorks; so are the shell mechanics —
// login-shell env, bounded logs, server spawn, readiness, external
// navigation, signal shutdown — through agentworks-desktop-lib
// (desktop/lib, a file: dependency), so a fix there reaches both apps.
// What stays here is what makes SparkQuill its own app: the product's
// ports, theme, tray/menus, close-to-tray, the voice-model lifecycle, and
// the isolation env for running next to AgentWorks.
//
// AGENT_PRODUCTS is deliberately left UNSET. Setting it to "sparkquill"
// alone would make isSingleProductServerDeployment() true and 400 every
// Claude Code turn with no stored OAuth token (see A3 in
// docs/design/sparkquill_desktop_on_platform_plan.md); the desktop case
// falls back to the user's locally logged-in CLI, exactly like AgentWorks.

const { app, BrowserWindow, shell, dialog, nativeTheme, Menu, Tray, nativeImage, ipcMain, Notification } = require('electron')
const path = require('path')
const fs = require('fs')
const crypto = require('crypto')
const {
  importLoginShellEnv,
  createBoundedLogWriter,
  waitForHealth,
  attachExternalNavigation,
  installSignalShutdown,
  spawnServer,
  killServer,
} = require('agentworks-desktop-lib')
const { checkForUpdates, startUpdateChecks, openReleaseNotes } = require('./updater')
const { buildAgentServerEnv } = require('./lib/agentEnv')

// A throwaway data directory (a visual pass on onboarding, a fresh-install
// check) without touching the family's real userData. Development only; a
// packaged app never sets it. Must run before the app is ready.
if (process.env.SPARKQUILL_USER_DATA_DIR) app.setPath('userData', process.env.SPARKQUILL_USER_DATA_DIR)

// Native notification for a message Quill sent the parent (preload's
// `notify`). Best effort: a machine that refuses notifications just keeps
// the in-app copy, which stays until the parent dismisses it.
ipcMain.on('sparkquill-notify', (_event, payload) => {
  try {
    if (!Notification.isSupported()) return
    const title = String(payload?.title || 'SparkQuill').slice(0, 120)
    const body = String(payload?.body || '').slice(0, 400)
    const n = new Notification({ title, body, silent: false })
    n.on('click', () => { if (mainWindow) { mainWindow.show(); mainWindow.focus() } })
    n.show()
  } catch { /* never let a notification take the app down */ }
})

const HEALTH_TIMEOUT_MS = 90000
const HEALTH_POLL_MS = 500
const HEALTH_INITIAL_DELAY_MS = 1000

// Own preferred ports, one digit off AgentWorks' (45678/45679) — collisions
// are soft either way (the next free port is taken, which just resets that
// origin's localStorage/JWT), but a distinct pair means the common case of
// running both apps at once needs no fallback at all.
const PREFERRED_AGENT_PORT = 45778
const PREFERRED_WORKSPACE_PORT = 45779

let agentProcess = null
let workspaceProcess = null
let mainWindow = null
let agentPort = PREFERRED_AGENT_PORT
let workspacePort = PREFERRED_WORKSPACE_PORT
const workspaceApiToken = crypto.randomBytes(32).toString('hex')
let tray = null
// Closing the window only HIDES it (see the 'close' handler in createWindow),
// so the servers keep running and the menu-bar icon stays. This flag is what
// distinguishes "the parent closed the window" from a real quit, which is the
// only time the window is allowed to actually close.
let isQuitting = false

function resourcesDir() {
  return app.isPackaged ? process.resourcesPath : path.join(__dirname, 'resources')
}

function logWriter(name) {
  return createBoundedLogWriter(path.join(app.getPath('userData'), 'logs', `${name}.log`), { appName: 'SparkQuill' })
}

// --- settings + auth secret ---------------------------------------------------
// The agent server fatals on start with an empty/default AUTH_SECRET
// (ValidateConfiguredAuthSecret) — the standalone family-server never needed
// one, so the shell mints and persists its own the first time it runs the
// platform binaries.
function settingsPath() {
  return path.join(app.getPath('userData'), 'config.json')
}

function loadSettings() {
  try {
    if (fs.existsSync(settingsPath())) return JSON.parse(fs.readFileSync(settingsPath(), 'utf8'))
  } catch (e) {
    console.error('[main] Failed to load settings:', e)
  }
  return {}
}

function saveSettings(settings) {
  fs.mkdirSync(path.dirname(settingsPath()), { recursive: true })
  fs.writeFileSync(settingsPath(), JSON.stringify(settings, null, 2))
}

function resolveAuthSecret() {
  const settings = loadSettings()
  if (settings.authSecret) return settings.authSecret
  const secret = crypto.randomBytes(32).toString('hex')
  saveSettings({ ...settings, authSecret: secret })
  return secret
}

function workspaceDocsDir() {
  return path.join(app.getPath('userData'), 'workspace-docs')
}

// --- servers -------------------------------------------------------------
async function spawnWorkspace() {
  const bin = path.join(resourcesDir(), 'workspace-server')
  if (!fs.existsSync(bin)) {
    throw new Error(`workspace-server not found at ${bin}.\n\nRun desktop-sparkquill/dev-setup.sh to build it.`)
  }
  const docsDir = workspaceDocsDir()
  const dataDir = path.join(app.getPath('userData'), 'data')
  fs.mkdirSync(dataDir, { recursive: true })
  const env = {
    ...process.env,
    DOCS_DIR: docsDir,
    DATA_DIR: dataDir,
    // workspace-server executes /api/execute shell commands. Mark it native
    // so the safe shell env preserves the imported login-shell PATH/HOME
    // instead of the Docker-style minimal PATH.
    NATIVE_WORKSPACE: 'true',
    WORKSPACE_API_TOKEN: workspaceApiToken,
  }
  const { child, port } = await spawnServer({
    name: 'workspace', bin, args: ['server', '--port', spawnServer.PORT_PLACEHOLDER, '--docs-dir', docsDir],
    preferredPort: PREFERRED_WORKSPACE_PORT, cwd: resourcesDir(), env, log: logWriter('workspace'),
    onExit: () => { workspaceProcess = null },
  })
  workspaceProcess = child
  workspacePort = port
}

async function spawnAgent() {
  const bin = path.join(resourcesDir(), 'agent-server')
  if (!fs.existsSync(bin)) {
    throw new Error(`agent-server not found at ${bin}.\n\nRun desktop-sparkquill/dev-setup.sh to build it.`)
  }
  // Resources dir is what makes server.go's `./static/` mount resolve to
  // the frontend staged there by dev-setup.sh/CI.
  const cwd = resourcesDir()
  const docsDir = workspaceDocsDir()
  const log = logWriter('agent')

  const configDir = path.join(app.getPath('userData'), 'configs')
  fs.mkdirSync(configDir, { recursive: true })
  const mcpConfigPath = path.join(configDir, 'mcp_servers.json')
  if (!fs.existsSync(mcpConfigPath)) {
    const defaultConfigPath = path.join(cwd, 'configs', 'mcp_servers_clean.json')
    try {
      if (fs.existsSync(defaultConfigPath)) fs.copyFileSync(defaultConfigPath, mcpConfigPath)
    } catch (err) {
      log.write(`[agent] failed to copy default mcp config: ${err}\n`)
    }
  }
  try {
    fs.mkdirSync(path.join(docsDir, 'Downloads'), { recursive: true })
  } catch { /* non-fatal */ }

  const env = buildAgentServerEnv(process.env, {
    authSecret: resolveAuthSecret(),
    workspacePort,
    docsDir,
    logFile: log.file,
    workspaceApiToken,
  })
  // AGENT_PRODUCTS and MULTI_USER_MODE are deliberately absent from env —
  // see the file header and lib/agentEnv.test.js.
  const { child, port } = await spawnServer({
    name: 'agent', bin,
    args: ['server', '--port', spawnServer.PORT_PLACEHOLDER, '--log-file', log.file, '--log-level', 'debug', '--mcp-config', mcpConfigPath],
    preferredPort: PREFERRED_AGENT_PORT, cwd, env, log,
    onExit: () => { agentProcess = null },
  })
  agentProcess = child
  agentPort = port
}

function stopServers() {
  killServer(workspaceProcess); workspaceProcess = null
  killServer(agentProcess); agentProcess = null
}

function waitForServers() {
  return waitForHealth({
    checks: [
      { name: `agent (port ${agentPort})`, url: `http://127.0.0.1:${agentPort}/api/health` },
      { name: `workspace (port ${workspacePort})`, url: `http://127.0.0.1:${workspacePort}/health` },
    ],
    timeoutMs: HEALTH_TIMEOUT_MS,
    pollMs: HEALTH_POLL_MS,
    initialDelayMs: HEALTH_INITIAL_DELAY_MS,
    isAlive: () => (agentProcess === null || workspaceProcess === null ? 'a server exited before it became ready — see Help → Open Logs.' : null),
  })
}

// --- voice model lifecycle ---------------------------------------------------
// The speech model costs 15-20s to load and ~1 GB resident. Tie it to whether
// the app is actually on screen. /api/voice/* sits behind the platform's
// auth and the main process holds no login token, so the shell only tells the
// renderer that visibility changed (see preload.js); the app's
// platform/voiceLifecycle.ts, which does hold the token, calls unload/warm.
function voiceLifecycle(action) {
  mainWindow?.webContents.send('window-visibility', { visible: action === 'warm' })
}

// --- window ------------------------------------------------------------------
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 900,
    minHeight: 600,
    title: 'SparkQuill',
    icon: path.join(resourcesDir(), 'icons', 'icon.png'),
    backgroundColor: '#fbf7ef', // the app's own cream — avoids a white flash on load
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
    },
  })

  // Cache-bust per version: the origin never changes (same fixed port), so an
  // upgraded app would otherwise keep serving the previous build's JS.
  mainWindow.loadURL(`http://127.0.0.1:${agentPort}/?v=${app.getVersion()}`)
  attachExternalNavigation(mainWindow.webContents, shell)
  if (process.env.SPARKQUILL_LOG_CONSOLE) {
    mainWindow.webContents.on('console-message', (_e, level, message, line, sourceId) => {
      console.log(`[renderer] ${message} (${sourceId}:${line})`)
    })
  }
  // A renderer crash should recover, not strand the user on a blank window.
  mainWindow.webContents.on('render-process-gone', () => mainWindow?.webContents.reload())
  // Closing hides rather than quits: the servers keep running, so check-ins
  // stay live, and reopening from the menu bar is instant. Only a real quit —
  // the tray item, Cmd-Q, or a system shutdown — is allowed through.
  mainWindow.on('close', (e) => {
    if (isQuitting) return
    e.preventDefault()
    mainWindow?.hide()
  })
  mainWindow.on('closed', () => { mainWindow = null })
  mainWindow.on('hide', () => voiceLifecycle('unload'))
  mainWindow.on('show', () => voiceLifecycle('warm'))
}

// Reopen from the menu bar / Dock. The window is usually still alive and just
// hidden, so this is normally a show(); it only rebuilds when the window was
// genuinely destroyed.
function showWindow() {
  if (mainWindow) {
    mainWindow.show()
    mainWindow.focus()
    return
  }
  if (agentProcess || process.env.DEV_URL) createWindow()
}

function createTray() {
  if (tray) return
  const iconPath = path.join(resourcesDir(), 'icons', 'icon.png')
  if (!fs.existsSync(iconPath)) return
  const icon = nativeImage.createFromPath(iconPath)
  if (icon.isEmpty()) return
  // 18px is the usable height of the macOS menu bar — the source icon is
  // 1024px square, and handing Tray the full-size image gets it scaled badly.
  tray = new Tray(icon.resize({ width: 18, height: 18 }))
  tray.setToolTip('SparkQuill')
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: 'Open SparkQuill', click: showWindow },
    { type: 'separator' },
    { label: 'Quit SparkQuill', click: () => { isQuitting = true; app.quit() } },
  ]))
  tray.on('click', showWindow)
}

function buildMenu() {
  const isMac = process.platform === 'darwin'
  const template = [
    // Spelled out rather than `{ role: 'appMenu' }` for one reason: macOS
    // convention puts "Check for Updates…" in the app menu directly under
    // About, and that is the first place anyone looks for it — but the
    // appMenu role is opaque, so an item cannot be inserted into it.
    ...(isMac
      ? [{
          label: app.name,
          submenu: [
            { role: 'about' },
            { type: 'separator' },
            { label: 'Check for Updates…', click: () => checkForUpdates(true) },
            { type: 'separator' },
            { role: 'services' },
            { type: 'separator' },
            { role: 'hide' },
            { role: 'hideOthers' },
            { role: 'unhide' },
            { type: 'separator' },
            { role: 'quit' },
          ],
        }]
      : []),
    { role: 'editMenu' },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        { role: 'resetZoom' }, { role: 'zoomIn' }, { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    { role: 'windowMenu' },
    {
      role: 'help',
      submenu: [
        ...(isMac ? [] : [{ label: 'Check for Updates…', click: () => checkForUpdates(true) }]),
        { label: 'Release Notes', click: openReleaseNotes },
        { type: 'separator' },
        { label: 'Open Logs', click: () => shell.openPath(path.join(app.getPath('userData'), 'logs')) },
        { label: 'Open SparkQuill Folder', click: () => shell.openPath(workspaceDocsDir()) },
      ],
    },
  ]
  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}

function failFast(err) {
  dialog.showErrorBox('SparkQuill could not start', String(err?.message || err))
  stopServers()
  app.exit(1)
}

importLoginShellEnv()

app.whenReady().then(async () => {
  nativeTheme.themeSource = 'light' // the app is designed light-first
  // The window's own `icon` option doesn't affect the macOS Dock icon while
  // running unpackaged; packaged builds bake it into the bundle.
  if (!app.isPackaged) app.dock?.setIcon(path.join(resourcesDir(), 'icons', 'icon.png'))
  buildMenu()
  createTray()
  try {
    // DEV_URL points at the Vite dev server and skips spawning entirely, so
    // desktop chrome can be worked on against a hot-reloading frontend.
    if (process.env.DEV_URL) {
      agentPort = Number(new URL(process.env.DEV_URL).port) || PREFERRED_AGENT_PORT
      createWindow()
      mainWindow.loadURL(process.env.DEV_URL)
      return
    }
    await spawnWorkspace()
    await spawnAgent()
    await waitForServers()
    createWindow()
    startUpdateChecks()
  } catch (err) {
    failFast(err)
  }
})

app.on('activate', showWindow)
// Deliberately NOT quitting when the last window goes: closing only hides the
// window, and the running servers are what drive check-ins — so they stay up,
// reachable from the menu bar, until the parent actually quits.
app.on('window-all-closed', () => {})
app.on('before-quit', () => { isQuitting = true; stopServers() })
app.on('will-quit', stopServers)
installSignalShutdown({ app, stop: () => { isQuitting = true; stopServers() } })
