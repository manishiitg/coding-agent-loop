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
  return { ghToken: '', ghRepo: '', docsDir: '', authSecret: '', schedulerEnabled: true };
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
      WORKSPACE_ENABLE_GITHUB_SYNC: 'true'
    };

    if (settings.ghToken) env.GITHUB_TOKEN = settings.ghToken;
    if (settings.ghRepo) env.GITHUB_REPO = settings.ghRepo;

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

    // Port resolved below via detect() — prefer fixed 45678 so frontend localStorage persists.
    const args = [
      'server',
      '--port', '0',
      '--log-file', logFile,
      '--log-level', 'debug',
      '--mcp-config', mcpConfigPath
    ];

    // Load Settings
    const settings = loadSettings();
    // In desktop mode, both agent-server and workspace-server run as native binaries
    // (no Docker). WORKSPACE_DOCS_PATH tells the agent the real filesystem path so
    // LLM-generated shell commands (jq, cat, etc.) use the correct absolute paths.
    // This same path is passed to workspace-server via --docs-dir in spawnWorkspace().
    const docsDir = process.env.RUNLOOP_DOCS_DIR || path.join(userDataPath, 'workspace-docs');
    const env = {
      ...process.env,
      WORKSPACE_API_URL: `http://127.0.0.1:${dynamicWorkspacePort}`,
      WORKSPACE_DOCS_PATH: docsDir,
      LOG_FILE: logFile,
      WORKSPACE_ENABLE_GITHUB_SYNC: 'true'
    };

    if (settings.ghToken) env.GITHUB_TOKEN = settings.ghToken;

    // Per-machine scheduler toggle. When the user disables this in Settings,
    // automatic cron execution stops on this machine; manual runs still work.
    if (settings.schedulerEnabled === false) env.SCHEDULER_ENABLED = 'false';

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
function checkForUpdates() {
  if (!app.isPackaged) {
    console.log('[update] Skipping check — not packaged');
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
      if (res.statusCode !== 200) return;
      let release;
      try { release = JSON.parse(data); } catch (e) {
        console.error('[update] parse error:', e);
        return;
      }
      const latestVersion = (release.tag_name || '').replace(/^v/, '');
      const currentVersion = app.getVersion();
      if (!latestVersion || !isNewerVersion(latestVersion, currentVersion)) {
        return;
      }
      const choice = await dialog.showMessageBox({
        type: 'info',
        title: 'Update Available',
        message: `Runloop ${latestVersion} is available.`,
        detail: `You're on ${currentVersion}. Click Update to download and install in the background — Runloop will relaunch automatically when ready.`,
        buttons: ['Update', 'Later'],
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

  // Give the installer a moment to fork the curl, then quit so install.sh's
  // pkill doesn't race with us.
  setTimeout(() => app.quit(), 500);
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
    console.error('[main] Window failed to load:', { errorCode, errorDescription, validatedURL });
  });

  mainWindow.webContents.on('render-process-gone', (_event, details) => {
    console.error('[main] Renderer process gone:', details);
  });

  mainWindow.webContents.on('unresponsive', () => {
    console.error('[main] Window became unresponsive');
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
