const { app, BrowserWindow, dialog, shell, nativeTheme, Menu, ipcMain } = require('electron');
const path = require('path');
const { spawn } = require('child_process');
const http = require('http');
const https = require('https');
const detect = require('detect-port');
const fs = require('fs');
const { Client } = require('pg');

// Dynamic ports (assigned at runtime)
let dynamicAgentPort = 0;
let dynamicWorkspacePort = 0;

const HEALTH_TIMEOUT_MS = 90000;
const HEALTH_POLL_MS = 500;
const HEALTH_INITIAL_DELAY_MS = 3000;

// Enforce dark mode for system UI (title bar, context menus)
nativeTheme.themeSource = 'dark';

let workspaceProcess = null;
let agentProcess = null;
let mainWindow = null;
let settingsWindow = null;

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
  return { dbType: 'sqlite', dbUrl: '', ghToken: '', ghRepo: '' };
}

function saveSettings(settings) {
  const configPath = path.join(app.getPath('userData'), 'config.json');
  fs.writeFileSync(configPath, JSON.stringify(settings, null, 2));
}

// IPC Handlers for Settings
ipcMain.handle('get-settings', () => loadSettings());
ipcMain.handle('test-db-connection', async (event, connectionString) => {
  const client = new Client({
    connectionString,
    connectionTimeoutMillis: 5000, // 5s timeout
  });
  try {
    await client.connect();
    await client.query('SELECT 1');
    await client.end();
    return { success: true };
  } catch (error) {
    return { success: false, error: error.message };
  }
});

ipcMain.on('save-settings', (event, settings) => {
  saveSettings(settings);
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

// isPortInUse function is no longer needed with dynamic ports
// but we keep it if we ever need to check specific ports

function spawnWorkspace(userDataPath) {
  return new Promise((resolve, reject) => {
    const bin = getBinaryPath('workspace-server');
    if (!fs.existsSync(bin)) {
      return reject(new Error(`Workspace server binary not found at ${bin}. Place workspace-server in desktop/resources/ for development.`));
    }
    const docsDir = path.join(userDataPath, 'workspace-docs');
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

    // Use port 0 for dynamic allocation
    const child = spawn(bin, ['server', '--port', '0', '--docs-dir', docsDir, '--data-dir', dataDir], {
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

    const dbPath = path.join(userDataPath, 'chat_history.db');
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

    // Use port 0 for dynamic allocation
    const args = [
      'server', 
      '--port', '0',
      '--db-path', dbPath,
      '--log-file', logFile,
      '--log-level', 'debug',
      '--mcp-config', mcpConfigPath
    ];

    // Load Settings
    const settings = loadSettings();
    const env = {
      ...process.env,
      WORKSPACE_API_URL: `http://127.0.0.1:${dynamicWorkspacePort}`, // Inject dynamic workspace port
      DB_PATH: dbPath,
      LOG_FILE: logFile,
      WORKSPACE_ENABLE_GITHUB_SYNC: 'true'
    };

    if (settings.ghToken) env.GITHUB_TOKEN = settings.ghToken;

    if (settings.dbType === 'postgres' && settings.dbUrl) {
      env.DATABASE_URL = settings.dbUrl;
      env.DB_TYPE = 'postgres';
      // Remove db-path arg if using postgres to avoid confusion (though agent priority logic handles it)
      const dbPathIndex = args.indexOf('--db-path');
      if (dbPathIndex !== -1) {
        args.splice(dbPathIndex, 2);
      }
      args.push('--db-type', 'postgres');
    }

    const child = spawn(bin, args, {
      cwd,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    agentProcess = child;
    
    // Log startup info
    const startupMsg = `[agent] Spawning agent-server with db-path=${dbPath}, log-file=${logFile}\n`;
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
      
      // Parse dynamic port
      if (!portFound) {
        const match = output.match(/DynamicPort: (\d+)/);
        if (match) {
          dynamicAgentPort = parseInt(match[1], 10);
          console.log(`[main] Agent server started on dynamic port: ${dynamicAgentPort}`);
          portFound = true;
          resolve();
        }
      }
    });
    
    child.stderr.on('data', (d) => {
      process.stderr.write(`[agent] ${d}`);
      logStream.write(d);
    });
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

function checkForUpdates() {
  const options = {
    hostname: 'api.github.com',
    path: '/repos/manishiitg/mcp-agent-builder-go/releases/latest',
    method: 'GET',
    headers: { 'User-Agent': 'MCP-Agent-Builder' }
  };

  const req = https.request(options, (res) => {
    let data = '';
    res.on('data', (chunk) => data += chunk);
    res.on('end', () => {
      if (res.statusCode === 200) {
        try {
          const release = JSON.parse(data);
          const latestVersion = release.tag_name.replace(/^v/, '');
          const currentVersion = app.getVersion();

          // Simple semantic version comparison
          // Assumes standard semver (e.g., 0.1.0)
          // If complex versions are needed, use 'semver' package
          if (latestVersion !== currentVersion && latestVersion > currentVersion) {
            const choice = dialog.showMessageBoxSync({
              type: 'info',
              title: 'Update Available',
              message: `Version ${latestVersion} is available.`,
              detail: `You are currently on version ${currentVersion}. Would you like to download the update?`,
              buttons: ['Download', 'Skip']
            });

            if (choice === 0) {
              shell.openExternal(release.html_url);
            }
          }
        } catch (e) {
          console.error('Failed to parse update info:', e);
        }
      }
    });
  });
  
  req.on('error', (e) => console.error('Update check failed:', e));
  req.end();
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
    title: 'AgentForge',
    message: 'Startup failed',
    detail: message,
  });
  killChildren();
  app.exit(1);
}

function createWindow(initialUrl) {
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
  if (devUrl) {
    // Open DevTools automatically in dev mode
    mainWindow.webContents.openDevTools({ mode: 'detach' });
  }

  // Always allow DevTools via Cmd+Shift+I / Ctrl+Shift+I
  mainWindow.webContents.on('before-input-event', (event, input) => {
    if ((input.meta || input.control) && input.shift && input.key === 'I') {
      mainWindow.webContents.toggleDevTools();
    }
  });

  mainWindow.loadURL(initialUrl || `http://127.0.0.1:${dynamicAgentPort}`);
  
  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

app.whenReady().then(async () => {
  createMenu();
  
  // If DEV_URL is set (e.g. http://localhost:5173), skip spawning servers and just load that URL
  const devUrl = process.env.DEV_URL;
  if (devUrl) {
    console.log(`[main] DEV_URL detected: ${devUrl}. Skipping local server spawn.`);
    createWindow(devUrl);
    return;
  }
  
  const userDataPath = app.getPath('userData');

  // 1. (Check ports removed - not needed for dynamic ports)

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
  app.quit();
});

app.on('before-quit', () => {
  killChildren();
});

app.on('will-quit', () => {
  killChildren();
});
