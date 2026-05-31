const { app, BrowserWindow, dialog, shell, nativeTheme, Menu, Tray, ipcMain, nativeImage } = require('electron');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const http = require('http');
const https = require('https');
const detect = require('detect-port');
const fs = require('fs');

// Dynamic ports (assigned at runtime)
let dynamicAgentPort = 0;
let dynamicWorkspacePort = 0;

const HEALTH_TIMEOUT_MS = 90000;
const HEALTH_POLL_MS = 500;
const HEALTH_INITIAL_DELAY_MS = 3000;

// Enforce dark mode for system UI (title bar, context menus)
nativeTheme.themeSource = 'dark';

// GUI-launched Mac apps inherit a minimal PATH (no Homebrew, no nvm, no ~/.local/bin),
// so spawned tools like `claude`, `npx`, etc. are not found. Read PATH from the user's
// login shell once at startup and use it for all spawned children.
function resolveLoginEnv() {
  if (process.platform !== 'darwin' && process.platform !== 'linux') return {};
  const shellBin = process.env.SHELL || '/bin/zsh';
  // Wrap printenv with unique markers so we can isolate the env block even if
  // the user's .zshrc/.bashrc echoes extra text to stdout (e.g. ssh-agent banners).
  const BEGIN = '__RL_ENV_BEGIN__';
  const END = '__RL_ENV_END__';
  try {
    const result = spawnSync(shellBin, ['-ilc', `printf '%s' '${BEGIN}'; /usr/bin/env -0; printf '%s' '${END}'`], {
      encoding: 'buffer',
      timeout: 4000,
      maxBuffer: 4 * 1024 * 1024,
    });
    const stdout = result.stdout ? result.stdout.toString('binary') : '';
    const beginIdx = stdout.indexOf(BEGIN);
    const endIdx = stdout.indexOf(END);
    if (beginIdx === -1 || endIdx === -1) return {};
    const block = stdout.slice(beginIdx + BEGIN.length, endIdx);
    const out = {};
    for (const entry of block.split('\0')) {
      if (!entry) continue;
      const eq = entry.indexOf('=');
      if (eq <= 0) continue;
      out[entry.slice(0, eq)] = entry.slice(eq + 1);
    }
    return out;
  } catch (e) {
    console.warn('[main] Failed to resolve login shell env:', e);
    return {};
  }
}
const LOGIN_ENV = resolveLoginEnv();
// Merge into process.env so spawned children pick up PATH + API keys + any other vars
// the user has in their login shell. Existing process.env values win (don't clobber).
for (const [k, v] of Object.entries(LOGIN_ENV)) {
  if (process.env[k] === undefined) process.env[k] = v;
}
if (LOGIN_ENV.PATH) process.env.PATH = LOGIN_ENV.PATH; // PATH must always come from login shell
console.log('[main] Imported', Object.keys(LOGIN_ENV).length, 'env vars from login shell');

let workspaceProcess = null;
let agentProcess = null;
let mainWindow = null;
let settingsWindow = null;
let tray = null;

// --- Diagnostic logging -----------------------------------------------------
// Console output is lost when the renderer blanks out and DevTools/terminal
// can't be opened. Mirror crash/error diagnostics to <userData>/logs/main.log
// so a post-mortem is always available. Best-effort; the logger never throws.
let diagLogStream = null;
function safeStringify(value) {
  try { return JSON.stringify(value); } catch (_e) { return String(value); }
}
function diagLog(...args) {
  const text = args.map((a) => (typeof a === 'string' ? a : safeStringify(a))).join(' ');
  const line = `[${new Date().toISOString()}] ${text}\n`;
  console.error(line.trimEnd());
  try {
    if (!diagLogStream) {
      const logsDir = path.join(app.getPath('userData'), 'logs');
      if (!fs.existsSync(logsDir)) fs.mkdirSync(logsDir, { recursive: true });
      diagLogStream = fs.createWriteStream(path.join(logsDir, 'main.log'), { flags: 'a' });
    }
    diagLogStream.write(line);
  } catch (_e) {
    /* best-effort: never let logging crash the app */
  }
}

// Renderer forwards uncaught errors / unhandled rejections / React error-boundary
// catches here (via the electronAPI.logRendererError preload bridge), so a blank
// screen leaves a trace even with no DevTools.
ipcMain.on('renderer-error', (_event, payload) => {
  diagLog('[renderer-error]', payload);
});

// Opt-in out-of-band debugging: launch with ELECTRON_REMOTE_DEBUG_PORT=9222 and
// attach Chrome at http://localhost:9222 even when the window is wedged/blank.
if (process.env.ELECTRON_REMOTE_DEBUG_PORT) {
  app.commandLine.appendSwitch('remote-debugging-port', process.env.ELECTRON_REMOTE_DEBUG_PORT);
}

function migrateLegacyUserData() {
  const userDataPath = app.getPath('userData');
  const legacyProductName = ['Agent', 'Forge'].join('');
  const legacyUserDataPath = path.join(path.dirname(userDataPath), legacyProductName);

  if (legacyUserDataPath === userDataPath || !fs.existsSync(legacyUserDataPath)) {
    return;
  }

  const hasRunloopData = fs.existsSync(userDataPath) && fs.readdirSync(userDataPath).length > 0;
  if (hasRunloopData) {
    return;
  }

  try {
    fs.mkdirSync(userDataPath, { recursive: true });
    fs.cpSync(legacyUserDataPath, userDataPath, { recursive: true, errorOnExist: false });
    console.log(`[main] Migrated legacy user data from ${legacyUserDataPath} to ${userDataPath}`);
  } catch (error) {
    console.warn('[main] Failed to migrate legacy Runloop user data:', error);
  }
}

// Settings Management
function loadSettings() {
  const configPath = path.join(app.getPath('userData'), 'config.json');
  try {
    if (fs.existsSync(configPath)) {
      return JSON.parse(fs.readFileSync(configPath, 'utf8'));
    }
  } catch (e) {
    console.error('Failed to load settings:', e);
  }
  return { docsDir: '', authSecret: '', schedulerAllowedWorkflows: '', logAgentPrompts: false };
}

// Show a modal asking the user for the AUTH_SECRET used to encrypt provider keys.
// mode='unlock' (existing encrypted file) or 'create' (no file yet — just choose a secret).
// Resolves with the entered value (empty string = skip).
function promptAuthSecret(mode) {
  return new Promise((resolve) => {
    const win = new BrowserWindow({
      width: 460,
      height: 320,
      resizable: false,
      modal: true,
      parent: mainWindow || undefined,
      title: mode === 'unlock' ? 'Unlock Workspace Keys' : 'Set Workspace Encryption Secret',
      webPreferences: { nodeIntegration: true, contextIsolation: false },
    });
    let answered = false;
    const finish = (value) => {
      if (answered) return;
      answered = true;
      ipcMain.removeListener('auth-secret-result', handler);
      try { win.close(); } catch (_) {}
      resolve(value);
    };
    const handler = (_event, value) => finish(value);
    ipcMain.on('auth-secret-result', handler);
    win.on('closed', () => finish(''));
    win.loadFile(path.join(__dirname, 'auth-prompt.html'), { hash: mode });
  });
}

// Resolve workspace docs dir on first launch.
// Precedence: RUNLOOP_DOCS_DIR env > settings.docsDir > prompt user to pick > default (userData/workspace-docs)
async function resolveDocsDir() {
  if (process.env.RUNLOOP_DOCS_DIR) return process.env.RUNLOOP_DOCS_DIR;

  const settings = loadSettings();
  if (settings.docsDir && fs.existsSync(settings.docsDir)) return settings.docsDir;

  // First launch (or path was deleted): ask user to choose.
  const choice = await dialog.showMessageBox({
    type: 'question',
    title: 'Choose workspace folder',
    message: 'Where should Runloop store your workspace documents?',
    detail: 'Pick an existing folder to use, or let Runloop create a default one in your application data directory.',
    buttons: ['Choose folder…', 'Use default'],
    defaultId: 0,
    cancelId: 1,
  });

  let chosen;
  if (choice.response === 0) {
    const result = await dialog.showOpenDialog({
      title: 'Select workspace-docs folder',
      properties: ['openDirectory', 'createDirectory'],
    });
    if (!result.canceled && result.filePaths[0]) chosen = result.filePaths[0];
  }
  if (!chosen) chosen = path.join(app.getPath('userData'), 'workspace-docs');

  fs.mkdirSync(chosen, { recursive: true });
  saveSettings({ ...settings, docsDir: chosen });
  return chosen;
}

function saveSettings(settings) {
  const configPath = path.join(app.getPath('userData'), 'config.json');
  fs.writeFileSync(configPath, JSON.stringify(settings, null, 2));
}

// IPC Handlers for Settings
ipcMain.handle('get-settings', () => loadSettings());
ipcMain.handle('get-app-version', () => app.getVersion());
ipcMain.handle('pick-docs-dir', async () => {
  const result = await dialog.showOpenDialog({
    title: 'Select workspace-docs folder',
    properties: ['openDirectory', 'createDirectory'],
  });
  if (result.canceled || !result.filePaths[0]) return null;
  return result.filePaths[0];
});

ipcMain.on('save-settings', (event, settings) => {
  saveSettings(settings);
  if (settings.docsDir) process.env.RUNLOOP_DOCS_DIR = settings.docsDir;
  if (settings.authSecret !== undefined) process.env.AUTH_SECRET = settings.authSecret;
  if (settingsWindow) settingsWindow.close();
  
  // Restart servers to apply changes
  dialog.showMessageBox(mainWindow, {
    type: 'info',
    title: 'Restart Required',
    message: 'Settings saved. The application servers will now restart to apply changes.',
    buttons: ['OK']
  }).then(() => {
    restartServers();
  });
});

// IPC Handler for dynamic workspace port
ipcMain.on('get-workspace-port', (event) => {
  event.returnValue = process.env.DEV_URL ? 8081 : dynamicWorkspacePort;
});

function openSettingsWindow() {
  if (settingsWindow) {
    settingsWindow.focus();
    return;
  }

  settingsWindow = new BrowserWindow({
    width: 500,
    height: 600,
    title: 'Settings',
    backgroundColor: '#1e1e1e',
    parent: mainWindow,
    modal: true,
    webPreferences: {
      nodeIntegration: true,
      contextIsolation: false // Simplifies simple settings UI
    }
  });

  settingsWindow.loadFile(path.join(__dirname, 'settings.html'));
  settingsWindow.on('closed', () => {
    settingsWindow = null;
  });
}

function restartServers() {
  killChildren();
  // Small delay to ensure ports freed
  setTimeout(() => {
    const userDataPath = app.getPath('userData');
    spawnWorkspace(userDataPath)
      .then(() => spawnAgent(userDataPath))
      .then(() => {
        const agentHealthUrl = `http://127.0.0.1:${dynamicAgentPort}/api/health`;
        const workspaceHealthUrl = `http://127.0.0.1:${dynamicWorkspacePort}/health`;
        return waitForHealth(agentHealthUrl, workspaceHealthUrl);
      })
      .then(() => {
        mainWindow.reload(); // Reload frontend to reconnect
      })
      .catch(err => {
        showErrorAndExit('Failed to restart servers: ' + err);
      });
  }, 2000); // Increased delay to 2s
}

// Setup Native Application Menu
function createMenu() {
  const isMac = process.platform === 'darwin';

  const template = [
    // { role: 'appMenu' }
    ...(isMac ? [{
      label: app.name,
      submenu: [
        { role: 'about' },
        { type: 'separator' },
        { label: 'Check for Updates…', click: () => checkForUpdates(true) },
        { type: 'separator' },
        { label: 'Settings...', click: openSettingsWindow },
        { type: 'separator' },
        { role: 'services' },
        { type: 'separator' },
        { role: 'hide' },
        { role: 'hideOthers' },
        { role: 'unhide' },
        { type: 'separator' },
        { role: 'quit' }
      ]
    }] : []),
    // { role: 'fileMenu' }
    {
      label: 'File',
      submenu: [
        { label: 'Check for Updates…', click: () => checkForUpdates(true) },
        { label: 'Settings...', click: openSettingsWindow },
        { type: 'separator' },
        { role: isMac ? 'close' : 'quit' }
      ]
    },
    // { role: 'editMenu' }
    {
      label: 'Edit',
      submenu: [
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        ...(isMac ? [
          { role: 'pasteAndMatchStyle' },
          { role: 'delete' },
          { role: 'selectAll' },
          { type: 'separator' },
          {
            label: 'Speech',
            submenu: [
              { role: 'startSpeaking' },
              { role: 'stopSpeaking' }
            ]
          }
        ] : [
          { role: 'delete' },
          { type: 'separator' },
          { role: 'selectAll' }
        ])
      ]
    },
    // { role: 'viewMenu' }
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' }
      ]
    },
    // { role: 'windowMenu' }
    {
      label: 'Window',
      submenu: [
        { role: 'minimize' },
        { role: 'zoom' },
        ...(isMac ? [
          { type: 'separator' },
          { role: 'front' },
          { type: 'separator' },
          { role: 'window' }
        ] : [
          { role: 'close' }
        ])
      ]
    },
    {
      role: 'help',
      submenu: [
        {
          label: 'Learn More',
          click: async () => {
            await shell.openExternal('https://github.com/manishiitg/mcp-agent-builder-go')
          }
        }
      ]
    }
  ];

  const menu = Menu.buildFromTemplate(template);
  Menu.setApplicationMenu(menu);
}

ipcMain.handle('print-to-pdf', async (event, suggestedFilename) => {
  const { canceled, filePath } = await dialog.showSaveDialog(mainWindow, {
    defaultPath: suggestedFilename,
    filters: [{ name: 'PDF Files', extensions: ['pdf'] }]
  })
  if (canceled || !filePath) return { canceled: true }
  const data = await event.sender.printToPDF({
    printBackground: true,
    pageSize: 'A4',
    margins: { marginType: 'custom', top: 15000, bottom: 15000, left: 10000, right: 10000 }
  })
  await require('fs').promises.writeFile(filePath, data)
  return { canceled: false }
})

ipcMain.handle('save-flow-image', async (event, { filename, dataUrl, format }) => {
  const extension = format === 'jpeg' ? 'jpg' : format === 'svg' ? 'svg' : 'png';
  const safeFilename = (filename || `workflow-flow.${extension}`).replace(/[\\/]/g, '-');
  const downloadsDir = app.getPath('downloads');
  const parsed = path.parse(safeFilename);
  let filePath = path.join(downloadsDir, safeFilename);
  let suffix = 1;
  while (fs.existsSync(filePath)) {
    filePath = path.join(downloadsDir, `${parsed.name}-${suffix}${parsed.ext || `.${extension}`}`);
    suffix += 1;
  }

  const rawData = String(dataUrl || '');
  const commaIndex = rawData.indexOf(',');
  const base64 = commaIndex >= 0 ? rawData.slice(commaIndex + 1) : rawData;
  const buffer = Buffer.from(base64, 'base64');
  if (format === 'png' && !buffer.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) {
    throw new Error('PNG export payload was invalid.');
  }
  if (format === 'svg' && !buffer.toString('utf8', 0, 128).trimStart().startsWith('<svg')) {
    throw new Error('SVG export payload was invalid.');
  }
  await fs.promises.writeFile(filePath, buffer);
  return { canceled: false, filePath };
});

ipcMain.handle('capture-flow-image', async (event, { filename, format, rect }) => {
  const extension = format === 'jpeg' ? 'jpg' : 'png';
  const safeFilename = (filename || `workflow-flow.${extension}`).replace(/[\\/]/g, '-');
  const downloadsDir = app.getPath('downloads');
  const parsed = path.parse(safeFilename);
  let filePath = path.join(downloadsDir, safeFilename);
  let suffix = 1;
  while (fs.existsSync(filePath)) {
    filePath = path.join(downloadsDir, `${parsed.name}-${suffix}${parsed.ext || `.${extension}`}`);
    suffix += 1;
  }

  const bounds = {
    x: Math.max(0, Math.round(rect?.x || 0)),
    y: Math.max(0, Math.round(rect?.y || 0)),
    width: Math.max(1, Math.round(rect?.width || 1)),
    height: Math.max(1, Math.round(rect?.height || 1)),
  };
  const image = await event.sender.capturePage(bounds);
  const buffer = format === 'jpeg' ? image.toJPEG(95) : image.toPNG();
  await fs.promises.writeFile(filePath, buffer);
  return { canceled: false, filePath };
});

// IPC Handler for Dock Badge
ipcMain.on('set-dock-badge', (event, text) => {
  if (process.platform === 'darwin') {
    app.dock.setBadge(text);
  }
});

// IPC Handler for opening external URLs
ipcMain.on('open-external', (event, url) => {
  console.log('[main] Received open-external request for:', url);
  shell.openExternal(url);
});

function getResourcesDir() {
  if (app.isPackaged) {
    return process.resourcesPath;
  }
  return path.join(__dirname, 'resources');
}

// Unify all agent log files under <userData>/logs/.
//
// The Go agent server hardcodes paths like `logs/llm_debug.log` and
// `logs/schedule.log` (also `logs/agent_prompts/` when LOG_AGENT_PROMPTS=true)
// relative to its cwd, which in packaged mode is the .app's Resources dir.
// That means those logs land *inside the app bundle* and get wiped on every
// reinstall/upgrade. We symlink Resources/logs → <userData>/logs so all log
// files end up in the same persistent place as agent.log + workspace.log.
//
// Idempotent: safe to call on every launch.
function unifyAgentLogsDir(userDataPath) {
  if (!app.isPackaged) return; // dev runs from agent_go/, leave logs there
  try {
    const userLogsDir = path.join(userDataPath, 'logs');
    const resourcesLogsDir = path.join(getResourcesDir(), 'logs');
    fs.mkdirSync(userLogsDir, { recursive: true });

    let stat;
    try { stat = fs.lstatSync(resourcesLogsDir); } catch (_) { stat = null; }

    if (stat && stat.isSymbolicLink()) {
      const current = fs.readlinkSync(resourcesLogsDir);
      if (current === userLogsDir) return; // already linked correctly
      fs.unlinkSync(resourcesLogsDir);
    } else if (stat && stat.isDirectory()) {
      // Move any existing files into userLogsDir (don't lose old logs), then rm dir.
      for (const entry of fs.readdirSync(resourcesLogsDir)) {
        const src = path.join(resourcesLogsDir, entry);
        const dst = path.join(userLogsDir, entry);
        try {
          if (!fs.existsSync(dst)) fs.renameSync(src, dst);
        } catch (e) {
          console.warn('[main] could not move existing log', entry, e.message);
        }
      }
      try { fs.rmSync(resourcesLogsDir, { recursive: true, force: true }); } catch (_) {}
    }

    fs.symlinkSync(userLogsDir, resourcesLogsDir, 'dir');
    console.log(`[main] Linked ${resourcesLogsDir} → ${userLogsDir}`);
  } catch (err) {
    console.warn('[main] Failed to unify logs dir:', err && err.message ? err.message : err);
  }
}

function getBinaryPath(name) {
  const base = getResourcesDir();
  return path.join(base, name);
}

function getAppIconPath() {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'icons', 'icon.png');
  }
  return path.join(__dirname, 'resources', 'icons', 'icon.png');
}

function applyAppIcon() {
  const iconPath = getAppIconPath();
  if (!fs.existsSync(iconPath)) {
    console.warn(`[main] App icon not found at ${iconPath}`);
    return;
  }

  const icon = nativeImage.createFromPath(iconPath);
  if (icon.isEmpty()) {
    console.warn(`[main] Failed to load app icon from ${iconPath}`);
    return;
  }

  if (process.platform === 'darwin' && app.dock) {
    app.dock.setIcon(icon);
  }
}

function openMainWindow() {
  if (mainWindow) {
    mainWindow.show();
    mainWindow.focus();
    return;
  }

  const devUrl = process.env.DEV_URL;
  if (devUrl) {
    createWindow(devUrl);
    return;
  }

  if (dynamicAgentPort) {
    createWindow();
  }
}

function createTray() {
  if (process.platform !== 'darwin' || tray) {
    return;
  }

  const iconPath = getAppIconPath();
  if (!fs.existsSync(iconPath)) {
    console.warn(`[main] Tray icon not found at ${iconPath}`);
    return;
  }

  const icon = nativeImage.createFromPath(iconPath);
  if (icon.isEmpty()) {
    console.warn(`[main] Failed to load tray icon from ${iconPath}`);
    return;
  }

  const trayIcon = icon.resize({ width: 18, height: 18 });

  tray = new Tray(trayIcon);
  tray.setToolTip('Runloop');
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: 'Open Runloop', click: openMainWindow },
    { type: 'separator' },
    { label: 'Quit Runloop (Stop Servers)', click: () => app.quit() },
  ]));
  tray.on('click', openMainWindow);
}

// isPortInUse function is no longer needed with dynamic ports
// but we keep it if we ever need to check specific ports

function spawnWorkspace(userDataPath) {
  return new Promise((resolve, reject) => {
    const bin = getBinaryPath('workspace-server');
    if (!fs.existsSync(bin)) {
      return reject(new Error(`Workspace server binary not found at ${bin}. Place workspace-server in desktop/resources/ for development.`));
    }
    const docsDir = process.env.RUNLOOP_DOCS_DIR || path.join(userDataPath, 'workspace-docs');
    const dataDir = path.join(userDataPath, 'data');
    const logsDir = path.join(userDataPath, 'logs');

    if (!fs.existsSync(dataDir)) {
      fs.mkdirSync(dataDir, { recursive: true });
    }
    if (!fs.existsSync(logsDir)) {
      fs.mkdirSync(logsDir, { recursive: true });
    }

    const logFile = path.join(logsDir, 'workspace.log');
    const logStream = fs.createWriteStream(logFile, { flags: 'a' });

    // Load Settings
    const settings = loadSettings();
    const env = { 
      ...process.env, 
      DOCS_DIR: docsDir, 
      DATA_DIR: dataDir,
      // workspace-server executes /api/execute shell commands. Mark it native so
      // the safe shell env preserves the imported login-shell PATH/HOME instead
      // of using the Docker-style minimal PATH.
      NATIVE_WORKSPACE: 'true'
    };

    // Prefer fixed port 45679 so frontend localStorage stays stable across launches.
    // detect() returns the preferred port if free, otherwise the next available one.
    detect(45679).then((port) => {
      const child = spawn(bin, ['server', '--port', String(port), '--docs-dir', docsDir], {
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      workspaceProcess = child;

    let portFound = false;

    child.on('error', (err) => {
      const msg = `[workspace] spawn error: ${err}\n`;
      console.error(msg);
      logStream.write(msg);
      if (!portFound) reject(err);
    });
    
    child.stdout.on('data', (d) => {
      const output = d.toString();
      process.stdout.write(`[workspace] ${output}`);
      logStream.write(output);
      
      // Parse dynamic port
      if (!portFound) {
        const match = output.match(/DynamicPort: (\d+)/);
        if (match) {
          dynamicWorkspacePort = parseInt(match[1], 10);
          console.log(`[main] Workspace server started on dynamic port: ${dynamicWorkspacePort}`);
          portFound = true;
          resolve();
        }
      }
    });
    
    child.stderr.on('data', (d) => {
      process.stderr.write(`[workspace] ${d}`);
      logStream.write(d);
    });
    }).catch(reject);
  });
}

function spawnAgent(userDataPath) {
  return new Promise((resolve, reject) => {
    const bin = getBinaryPath('agent-server');
    if (!fs.existsSync(bin)) {
      return reject(new Error(`Agent server binary not found at ${bin}. Place agent-server in desktop/resources/ for development.`));
    }
    const cwd = app.isPackaged ? getResourcesDir() : path.join(__dirname, '..', 'agent_go');
    
    // Ensure logs directory exists
    const logsDir = path.join(userDataPath, 'logs');
    if (!fs.existsSync(logsDir)) {
      fs.mkdirSync(logsDir, { recursive: true });
    }

    const logFile = path.join(logsDir, 'agent.log');
    const logStream = fs.createWriteStream(logFile, { flags: 'a' });

    // Load settings + resolve docsDir up-front so the MCP rewrite below
    // (and the env block further down) can both use them.
    const settings = loadSettings();
    const docsDir = process.env.RUNLOOP_DOCS_DIR || path.join(userDataPath, 'workspace-docs');

    // MCP Config handling
    const configDir = path.join(userDataPath, 'configs');
    if (!fs.existsSync(configDir)) {
      fs.mkdirSync(configDir, { recursive: true });
    }
    const mcpConfigPath = path.join(configDir, 'mcp_servers.json');

    // Copy default config if it doesn't exist in userData
    if (!fs.existsSync(mcpConfigPath)) {
      const defaultConfigPath = path.join(cwd, 'configs', 'mcp_servers_clean.json');
      if (fs.existsSync(defaultConfigPath)) {
        try {
          fs.copyFileSync(defaultConfigPath, mcpConfigPath);
          console.log(`[agent] Copied default config to ${mcpConfigPath}`);
        } catch (err) {
          console.error(`[agent] Failed to copy default config: ${err}`);
        }
      } else {
        console.warn(`[agent] Default MCP config not found at ${defaultConfigPath}`);
      }
    }

    // Rewrite relative `../workspace-docs` paths to the absolute resolved
    // docsDir, plus ensure the Downloads subdir exists. The default MCP
    // config (and many older user configs) hardcode `../workspace-docs/...`
    // which only resolves when the agent's cwd is `<repo>/agent_go/`. In the
    // packaged app the cwd is the .app's Resources dir, so the relative path
    // points at a non-existent location and stdio MCPs fail with "chdir ...:
    // no such file or directory". Patches both `working_dir` strings and any
    // arg values. Idempotent.
    try {
      fs.mkdirSync(path.join(docsDir, 'Downloads'), { recursive: true });
    } catch (e) {
      console.warn('[agent] Could not ensure Downloads dir:', e.message);
    }
    try {
      let raw = fs.readFileSync(mcpConfigPath, 'utf8');
      const original = raw;

      // (a) Migrate legacy relative `../workspace-docs` to absolute current docsDir.
      raw = raw.replaceAll('../workspace-docs', docsDir);

      // (b) Migrate any previously-recorded absolute docsDir to the new one.
      // This is what makes the rewrite dynamic when the user changes their
      // workspace folder via Settings → Workspace Folder → Change…
      if (settings.previousDocsDir && settings.previousDocsDir !== docsDir) {
        raw = raw.replaceAll(settings.previousDocsDir, docsDir);
      }

      if (raw !== original) {
        fs.writeFileSync(mcpConfigPath, raw);
        console.log(`[agent] Rewrote workspace-docs paths in mcp_servers.json → ${docsDir}`);
      }
    } catch (e) {
      console.warn('[agent] Could not rewrite mcp_servers.json:', e.message);
    }

    // Remember which docsDir we just substituted so the next spawn (after a
    // potential folder change) can migrate from it.
    if (settings.previousDocsDir !== docsDir) {
      saveSettings({ ...settings, previousDocsDir: docsDir });
    }

    // Port resolved below via detect() — prefer fixed 45678 so frontend localStorage persists.
    const args = [
      'server',
      '--port', '0',
      '--log-file', logFile,
      '--log-level', 'debug',
      '--mcp-config', mcpConfigPath
    ];

    // settings and docsDir are loaded earlier (before the MCP rewrite block).
    // WORKSPACE_DOCS_PATH below tells the agent the real filesystem path so
    // LLM-generated shell commands (jq, cat, etc.) use the correct absolute
    // paths. The same docsDir is also passed to workspace-server via --docs-dir
    // in spawnWorkspace().
    const env = {
      ...process.env,
      WORKSPACE_API_URL: `http://127.0.0.1:${dynamicWorkspacePort}`,
      WORKSPACE_DOCS_PATH: docsDir,
      DOCS_DIR: docsDir,
      LOG_FILE: logFile,
      // Both servers run as native binaries on the host (no Docker). Without
      // this, the agent assumes the workspace is in Docker and emits
      // host.docker.internal URLs in MCP_API_URL — which the LLM-generated
      // shell commands then fail to reach (host.docker.internal isn't
      // resolvable on macOS without Docker Desktop running).
      NATIVE_WORKSPACE: 'true'
    };

    if (settings.schedulerAllowedWorkflows) env.SCHEDULER_ALLOWED_WORKFLOWS = settings.schedulerAllowedWorkflows;

    // Debug: persist the final system prompt + user message + tool calls for
    // every LLM call to <cwd>/logs/agent_prompts/{session_id}/. Off by default
    // because output is verbose; useful when debugging why the LLM produced
    // a particular shell command or chose a tool.
    if (settings.logAgentPrompts === true) env.LOG_AGENT_PROMPTS = 'true';

    detect(45678).then((port) => {
      const portIdx = args.indexOf('--port');
      args[portIdx + 1] = String(port);

      const child = spawn(bin, args, {
        cwd,
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      agentProcess = child;

      const startupMsg = `[agent] Spawning agent-server with log-file=${logFile}, port=${port}\n`;
      console.log(startupMsg.trim());
      logStream.write(startupMsg);

      let portFound = false;

      child.on('error', (err) => {
        const msg = `[agent] spawn error: ${err}\n`;
        console.error(msg);
        logStream.write(msg);
        if (!portFound) reject(err);
      });

      child.stdout.on('data', (d) => {
        const output = d.toString();
        process.stdout.write(`[agent] ${output}`);
        logStream.write(output);

        if (!portFound) {
          const match = output.match(/DynamicPort: (\d+)/);
          if (match) {
            dynamicAgentPort = parseInt(match[1], 10);
            console.log(`[main] Agent server started on port: ${dynamicAgentPort}`);
            portFound = true;
            resolve();
          }
        }
      });

      child.stderr.on('data', (d) => {
        process.stderr.write(`[agent] ${d}`);
        logStream.write(d);
      });
    }).catch(reject);
  });
}

function fetchHealth(url) {
  return new Promise((resolve) => {
    const req = http.get(url, (res) => {
      resolve(res.statusCode === 200);
    });
    req.on('error', () => resolve(false));
    req.setTimeout(5000, () => {
      req.destroy();
      resolve(false);
    });
  });
}

function waitForHealth(agentUrl, workspaceUrl) {
  const deadline = Date.now() + HEALTH_TIMEOUT_MS;
  function poll() {
    if (Date.now() > deadline) {
      return Promise.all([fetchHealth(agentUrl), fetchHealth(workspaceUrl)]).then(([agentOk, workspaceOk]) => {
        const parts = [];
        if (!agentOk) parts.push('agent (port ' + dynamicAgentPort + ')');
        if (!workspaceOk) parts.push('workspace (port ' + dynamicWorkspacePort + ')');
        const which = parts.length ? parts.join(' and ') : 'one or both';
        return Promise.reject(new Error('Servers did not become ready in time. Not ready: ' + which + '. Ensure agent-server and workspace-server are in desktop/resources/.'));
      });
    }
    return Promise.all([fetchHealth(agentUrl), fetchHealth(workspaceUrl)]).then(([agentOk, workspaceOk]) => {
      if (agentOk && workspaceOk) return;
      return new Promise((r) => setTimeout(r, HEALTH_POLL_MS)).then(poll);
    });
  }
  return new Promise((r) => setTimeout(r, HEALTH_INITIAL_DELAY_MS)).then(poll);
}

// Update flow for the unsigned macOS build.
//
// We don't use electron-updater / Squirrel.Mac — those assume a signed +
// notarized app. With our ad-hoc build they fail unpredictably.
//
// Instead: poll GitHub Releases on startup; if a newer tag exists, prompt
// the user. On "Update" we spawn install.sh in a *detached* shell (nohup so
// it survives Runloop quitting) and quit ourselves. install.sh kills any
// leftover Runloop processes, downloads the new dmg, replaces
// /Applications/Runloop.app, strips quarantine, and relaunches.
function checkForUpdates(manual = false) {
  if (!app.isPackaged) {
    console.log('[update] Skipping check — not packaged');
    if (manual) {
      dialog.showMessageBox({
        type: 'info',
        title: 'Updates',
        message: 'Update checks are disabled in development mode.',
        buttons: ['OK'],
      });
    }
    return;
  }

  const options = {
    hostname: 'api.github.com',
    path: '/repos/manishiitg/mcp-agent-builder-go/releases/latest',
    method: 'GET',
    headers: { 'User-Agent': 'Runloop' }
  };

  const req = https.request(options, (res) => {
    let data = '';
    res.on('data', (chunk) => data += chunk);
    res.on('end', async () => {
      if (res.statusCode !== 200) {
        if (manual) {
          dialog.showErrorBox('Update check failed', `GitHub returned HTTP ${res.statusCode}.`);
        }
        return;
      }
      let release;
      try { release = JSON.parse(data); } catch (e) {
        console.error('[update] parse error:', e);
        if (manual) dialog.showErrorBox('Update check failed', 'Could not parse GitHub response.');
        return;
      }
      const latestVersion = (release.tag_name || '').replace(/^v/, '');
      const currentVersion = app.getVersion();
      if (!latestVersion || !isNewerVersion(latestVersion, currentVersion)) {
        if (manual) {
          dialog.showMessageBox({
            type: 'info',
            title: "You're up to date",
            message: `Runloop ${currentVersion} is the latest version.`,
            buttons: ['OK'],
          });
        }
        return;
      }
      const choice = await dialog.showMessageBox({
        type: 'info',
        title: 'Update Available',
        message: `Runloop ${latestVersion} is available.`,
        detail: `You're on ${currentVersion}.\n\nInstalling will quit Runloop right now, download the new version (~150 MB), and relaunch automatically. The whole thing takes ~30 seconds.\n\nAny in-progress chats or workflow runs will be interrupted.`,
        buttons: ['Quit & Install', 'Later'],
        defaultId: 0,
        cancelId: 1,
      });
      if (choice.response === 0) {
        runUpdaterAndQuit(latestVersion);
      }
    });
  });
  req.on('error', (e) => console.warn('[update] check failed:', e?.message || e));
  req.end();
}

// Compare semver-ish strings (e.g. "1.25.10" vs "1.25.9"). Numeric per dotted
// segment; missing segments treated as 0. Returns true if a > b.
function isNewerVersion(a, b) {
  const pa = a.split('.').map((n) => parseInt(n, 10) || 0);
  const pb = b.split('.').map((n) => parseInt(n, 10) || 0);
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const x = pa[i] || 0, y = pb[i] || 0;
    if (x > y) return true;
    if (x < y) return false;
  }
  return false;
}

function runUpdaterAndQuit(targetVersion) {
  // Detached child shell so install.sh keeps running after we quit.
  // RUNLOOP_VERSION pins the exact tag we just promised the user.
  const innerCmd = `export RUNLOOP_VERSION='v${targetVersion}'; curl -fsSL https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/install.sh | bash > /tmp/runloop-update.log 2>&1`;
  const wrapped = `nohup bash -c ${JSON.stringify(innerCmd)} >/dev/null 2>&1 &`;

  console.log('[update] spawning detached installer for v' + targetVersion);
  try {
    const child = spawn('/bin/bash', ['-lc', wrapped], { detached: true, stdio: 'ignore' });
    child.unref();
  } catch (err) {
    dialog.showErrorBox('Update failed to start', String(err?.message || err));
    return;
  }

  // Show a non-blocking native notification so the user sees confirmation
  // that the update started, even after the window closes.
  try {
    const { Notification } = require('electron');
    if (Notification.isSupported()) {
      new Notification({
        title: 'Updating Runloop…',
        body: `Downloading v${targetVersion}. The app will reopen automatically in ~30 seconds.`,
        silent: false,
      }).show();
    }
  } catch (_) {}

  // Tray tooltip update for ambient awareness while the app quits.
  if (tray) {
    try { tray.setToolTip(`Runloop — installing v${targetVersion}…`); } catch (_) {}
  }

  // Give the installer a moment to fork the curl + the user a moment to read
  // the notification, then quit so install.sh's pkill doesn't race with us.
  setTimeout(() => app.quit(), 1000);
}

function killChildren() {
  if (workspaceProcess) {
    try {
      workspaceProcess.kill('SIGTERM');
    } catch (_) {}
    workspaceProcess = null;
  }
  if (agentProcess) {
    try {
      agentProcess.kill('SIGTERM');
    } catch (_) {}
    agentProcess = null;
  }
}

function showErrorAndExit(message) {
  dialog.showMessageBoxSync({
    type: 'error',
    title: 'Runloop',
    message: 'Startup failed',
    detail: message,
  });
  killChildren();
  app.exit(1);
}

function createWindow(initialUrl) {
  const targetUrl = initialUrl || `http://127.0.0.1:${dynamicAgentPort}`;
  console.log('[main] Creating main window for:', targetUrl);

  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    backgroundColor: '#1e1e1e', // Dark background to match theme and prevent white flash
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
    },
  });
  
  console.log('[main] Initializing window handlers...');

  mainWindow.webContents.on('did-start-loading', () => {
    console.log('[main] Window started loading:', targetUrl);
  });

  mainWindow.webContents.on('did-finish-load', () => {
    console.log('[main] Window finished loading:', mainWindow?.webContents.getURL());
  });

  mainWindow.webContents.on('did-fail-load', (_event, errorCode, errorDescription, validatedURL) => {
    // -3 is ERR_ABORTED (benign — e.g. a canceled/replaced subframe load).
    if (errorCode === -3) return;
    diagLog('[main] Window failed to load:', { errorCode, errorDescription, validatedURL });
  });

  mainWindow.webContents.on('render-process-gone', (_event, details) => {
    diagLog('[main] Renderer process gone:', details);
    // Auto-recover a crashed renderer so the user isn't stranded on a blank
    // screen having to discover Ctrl+R themselves.
    if (details && details.reason !== 'clean-exit' && mainWindow && !mainWindow.isDestroyed()) {
      diagLog('[main] Auto-reloading renderer after crash');
      try { mainWindow.webContents.reload(); } catch (e) { diagLog('[main] auto-reload failed:', String(e)); }
    }
  });

  mainWindow.webContents.on('unresponsive', () => {
    diagLog('[main] Window became unresponsive');
  });

  mainWindow.webContents.on('responsive', () => {
    diagLog('[main] Window became responsive again');
  });

  mainWindow.webContents.on('preload-error', (_event, preloadPath, error) => {
    diagLog('[main] Preload error:', preloadPath, String((error && error.stack) || error));
  });

  // Handle new window requests (e.g. target="_blank" or window.open)
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    console.log('[main] setWindowOpenHandler intercepted request for:', url);
    
    // Check if it's an external URL (not our local server)
    if (url.startsWith('http://127.0.0.1') || url.startsWith('http://localhost')) {
      console.log('[main] Allowing internal window/popup for:', url);
      return { action: 'allow' };
    }

    console.log('[main] Opening external URL in system browser:', url);
    shell.openExternal(url).catch(err => {
      console.error('[main] Failed to open external URL:', err);
    });

    return { action: 'deny' };
  });

  // Same-window navigations — a plain <a href> click (e.g. a URL printed in
  // terminal output) would otherwise REPLACE the app in the Electron window.
  // Intercept external http(s) navigations and open them in the system browser;
  // let internal app / dev-server navigation proceed normally.
  const isInternalNavUrl = (u) =>
    u.startsWith('http://127.0.0.1') ||
    u.startsWith('http://localhost') ||
    u.startsWith('file://') ||
    u.startsWith('app://') ||
    u.startsWith('about:');
  const redirectExternalNavigation = (event, url) => {
    if (!isInternalNavUrl(url) && /^https?:\/\//i.test(url)) {
      event.preventDefault();
      console.log('[main] will-navigate → opening external URL in system browser:', url);
      shell.openExternal(url).catch(err => {
        console.error('[main] Failed to open external URL:', err);
      });
    }
  };
  mainWindow.webContents.on('will-navigate', redirectExternalNavigation);
  mainWindow.webContents.on('will-redirect', redirectExternalNavigation);

  const devUrl = process.env.DEV_URL;
  if (devUrl && process.env.ELECTRON_OPEN_DEVTOOLS === '1') {
    // DevTools is expensive on large workflow/event views; keep it opt-in.
    mainWindow.webContents.openDevTools({ mode: 'detach' });
  }

  // Always allow DevTools via Cmd+Shift+I / Ctrl+Shift+I
  mainWindow.webContents.on('before-input-event', (event, input) => {
    if ((input.meta || input.control) && input.shift && input.key === 'I') {
      mainWindow.webContents.toggleDevTools();
    }
  });

  mainWindow.loadURL(targetUrl);
  
  mainWindow.on('closed', () => {
    console.log('[main] Main window closed');
    mainWindow = null;
  });
}

app.whenReady().then(async () => {
  migrateLegacyUserData();
  applyAppIcon();
  createMenu();
  createTray();
  
  // If DEV_URL is set (e.g. http://localhost:5173), skip spawning servers and just load that URL
  const devUrl = process.env.DEV_URL;
  if (devUrl) {
    console.log(`[main] DEV_URL detected: ${devUrl}. Skipping local server spawn.`);
    createWindow(devUrl);
    return;
  }
  
  const userDataPath = app.getPath('userData');

  // Resolve workspace-docs dir (prompts user on first launch if needed).
  // Cached in process.env so spawn helpers pick it up consistently.
  let resolvedDocsDir;
  try {
    resolvedDocsDir = await resolveDocsDir();
    process.env.RUNLOOP_DOCS_DIR = resolvedDocsDir;
    console.log(`[main] Workspace docs dir: ${resolvedDocsDir}`);
  } catch (err) {
    showErrorAndExit('Failed to resolve workspace folder: ' + (err.message || err));
    return;
  }

  // Prompt the user for AUTH_SECRET on first launch (regardless of whether
  // provider-api-keys.json exists). 'unlock' mode if a file is already there,
  // 'create' mode if not. The secret is used to encrypt/decrypt the workspace
  // provider key store. Actual API keys (gemini, openai, etc.) are added later
  // via the in-app provider auth flow.
  const settings = loadSettings();
  if (!settings.authSecret && !process.env.AUTH_SECRET) {
    const providerKeysPath = path.join(resolvedDocsDir, 'config', 'provider-api-keys.json');
    const mode = fs.existsSync(providerKeysPath) ? 'unlock' : 'create';
    const entered = await promptAuthSecret(mode);
    if (entered) {
      saveSettings({ ...settings, authSecret: entered });
      process.env.AUTH_SECRET = entered;
    }
  } else if (settings.authSecret) {
    process.env.AUTH_SECRET = settings.authSecret;
  }

  // 2. Spawn servers (in sequence: Workspace first, then Agent)
  try {
    unifyAgentLogsDir(userDataPath);
    console.log('[main] Spawning local servers...');
    await spawnWorkspace(userDataPath);
    await spawnAgent(userDataPath);
  } catch (err) {
    showErrorAndExit(err.message || String(err));
    return;
  }

  // 3. Wait for health
  try {
    console.log('[main] Waiting for backend health...');
    const agentHealthUrl = `http://127.0.0.1:${dynamicAgentPort}/api/health`;
    const workspaceHealthUrl = `http://127.0.0.1:${dynamicWorkspacePort}/health`;
    await waitForHealth(agentHealthUrl, workspaceHealthUrl);
  } catch (err) {
    showErrorAndExit(err.message || String(err));
    return;
  }

  // 4. Create Window
  createWindow();
  checkForUpdates();
  // Re-check every 4 hours so long-running sessions notice new releases
  // without requiring a manual restart.
  setInterval(() => checkForUpdates(), 4 * 60 * 60 * 1000);
});

app.on('window-all-closed', () => {
  console.log('[main] window-all-closed');
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

app.on('activate', () => {
  console.log('[main] activate');
  openMainWindow();
});

app.on('before-quit', () => {
  console.log('[main] before-quit');
  killChildren();
});

app.on('will-quit', () => {
  console.log('[main] will-quit');
  killChildren();
});

app.on('quit', (_event, exitCode) => {
  console.log('[main] quit with exit code:', exitCode);
});
