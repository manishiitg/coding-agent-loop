const { app, BrowserWindow, dialog } = require('electron');
const path = require('path');
const { spawn } = require('child_process');
const http = require('http');
const detect = require('detect-port');
const fs = require('fs');

const AGENT_PORT = 45678;
const WORKSPACE_PORT = 45679;
const HEALTH_TIMEOUT_MS = 90000;
const HEALTH_POLL_MS = 500;
const HEALTH_INITIAL_DELAY_MS = 3000;

let workspaceProcess = null;
let agentProcess = null;
let mainWindow = null;

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

function isPortInUse(port) {
  return detect(port).then((available) => available !== port);
}

function checkPortsAvailable() {
  return Promise.all([
    isPortInUse(AGENT_PORT),
    isPortInUse(WORKSPACE_PORT),
  ]).then(([agentInUse, workspaceInUse]) => {
    if (agentInUse || workspaceInUse) {
      const which = [];
      if (agentInUse) which.push(`${AGENT_PORT}`);
      if (workspaceInUse) which.push(`${WORKSPACE_PORT}`);
      return Promise.reject(new Error(`Port(s) ${which.join(' and ')} are already in use. Please close the application using them and try again.`));
    }
  });
}

function spawnWorkspace(userDataPath) {
  const bin = getBinaryPath('workspace-server');
  if (!fs.existsSync(bin)) {
    return Promise.reject(new Error(`Workspace server binary not found at ${bin}. Place workspace-server in desktop/resources/ for development.`));
  }
  const docsDir = path.join(userDataPath, 'workspace-docs');
  const child = spawn(bin, ['server', '--port', String(WORKSPACE_PORT), '--docs-dir', docsDir], {
    env: { ...process.env, DOCS_DIR: docsDir },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  workspaceProcess = child;
  child.on('error', (err) => {
    console.error('[workspace] spawn error:', err);
  });
  child.stderr?.on('data', (d) => process.stderr.write(d));
  return Promise.resolve();
}

function spawnAgent(userDataPath) {
  const bin = getBinaryPath('agent-server');
  if (!fs.existsSync(bin)) {
    return Promise.reject(new Error(`Agent server binary not found at ${bin}. Place agent-server in desktop/resources/ for development.`));
  }
  const cwd = app.isPackaged ? getResourcesDir() : path.join(__dirname, '..', 'agent_go');
  
  // Ensure logs directory exists
  const logsDir = path.join(userDataPath, 'logs');
  if (!fs.existsSync(logsDir)) {
    fs.mkdirSync(logsDir, { recursive: true });
  }

  const dbPath = path.join(userDataPath, 'chat_history.db');
  const logFile = path.join(logsDir, 'agent.log');

  const args = [
    'server', 
    '--port', String(AGENT_PORT),
    '--db-path', dbPath,
    '--log-file', logFile,
    '--log-level', 'debug'
  ];

  const child = spawn(bin, args, {
    cwd,
    env: {
      ...process.env,
      WORKSPACE_API_URL: `http://127.0.0.1:${WORKSPACE_PORT}`,
      // Ensure specific env vars are set if needed
      DB_PATH: dbPath,
      LOG_FILE: logFile
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  agentProcess = child;
  
  // Log startup info
  console.log(`[agent] Spawning agent-server with db-path=${dbPath}, log-file=${logFile}`);

  child.on('error', (err) => {
    console.error('[agent] spawn error:', err);
  });
  child.stdout?.on('data', (d) => process.stdout.write(`[agent] ${d}`));
  child.stderr?.on('data', (d) => process.stderr.write(`[agent] ${d}`));
  return Promise.resolve();
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
        if (!agentOk) parts.push('agent (port ' + AGENT_PORT + ')');
        if (!workspaceOk) parts.push('workspace (port ' + WORKSPACE_PORT + ')');
        const which = parts.length ? parts.join(' and ') : 'one or both';
        return Promise.reject(new Error('Servers did not become ready in time. Not ready: ' + which + '. Ensure agent-server and workspace-server are in desktop/resources/ and that ports ' + AGENT_PORT + '/' + WORKSPACE_PORT + ' are free.'));
      });
    }
    return Promise.all([fetchHealth(agentUrl), fetchHealth(workspaceUrl)]).then(([agentOk, workspaceOk]) => {
      if (agentOk && workspaceOk) return;
      return new Promise((r) => setTimeout(r, HEALTH_POLL_MS)).then(poll);
    });
  }
  return new Promise((r) => setTimeout(r, HEALTH_INITIAL_DELAY_MS)).then(poll);
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
    title: 'MCP Agent Builder',
    message: 'Startup failed',
    detail: message,
  });
  killChildren();
  app.exit(1);
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
    },
  });
  mainWindow.loadURL(`http://127.0.0.1:${AGENT_PORT}`);
  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

app.whenReady().then(() => {
  const userDataPath = app.getPath('userData');

  checkPortsAvailable()
    .then(() => {
      return spawnWorkspace(userDataPath);
    })
    .then(() => {
      return spawnAgent(userDataPath);
    })
    .then(() => {
      const agentHealthUrl = `http://127.0.0.1:${AGENT_PORT}/api/health`;
      const workspaceHealthUrl = `http://127.0.0.1:${WORKSPACE_PORT}/health`;
      return waitForHealth(agentHealthUrl, workspaceHealthUrl);
    })
    .then(() => {
      createWindow();
    })
    .catch((err) => {
      showErrorAndExit(err.message || String(err));
    });
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
