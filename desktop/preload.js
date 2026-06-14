const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
  getApiBaseUrl: () => {
    // In dev mode, prefer the runtime-config file written by the launcher.
    const runtime = window.__APP_RUNTIME_CONFIG__;
    if (runtime?.apiBaseUrl) return runtime.apiBaseUrl;
    // Otherwise, use the port the window loaded from (dynamic agent port)
    return window.location.origin;
  },
  getWorkspaceApiBaseUrl: () => {
    const runtime = window.__APP_RUNTIME_CONFIG__;
    if (runtime?.workspaceApiBaseUrl) return runtime.workspaceApiBaseUrl;
    // Get the workspace port from main process (sync)
    const port = ipcRenderer.sendSync('get-workspace-port');
    return `http://127.0.0.1:${port}`;
  },
  getAppVersion: () => ipcRenderer.invoke('get-app-version'),
  setDockBadge: (text) => ipcRenderer.send('set-dock-badge', text),
  openExternal: (url) => ipcRenderer.send('open-external', url),
  printToPDF: (filename) => ipcRenderer.invoke('print-to-pdf', filename),
  saveFlowImage: (payload) => ipcRenderer.invoke('save-flow-image', payload),
  captureFlowImage: (payload) => ipcRenderer.invoke('capture-flow-image', payload),
  captureRegion: (payload) => ipcRenderer.invoke('capture-region', payload),
  // Forward uncaught renderer errors to the main process so they land in the
  // main log file even when DevTools can't be opened (blank-screen post-mortem).
  logRendererError: (payload) => ipcRenderer.send('renderer-error', payload),

  // Auto-update: subscribe to background-download progress and trigger install.
  // onUpdateProgress receives {status:'downloading'|'ready'|'error', version,
  // percent, transferred, total, message}. Returns an unsubscribe function.
  onUpdateProgress: (cb) => {
    const handler = (_e, payload) => cb(payload);
    ipcRenderer.on('update-progress', handler);
    return () => ipcRenderer.removeListener('update-progress', handler);
  },
  restartToInstall: () => ipcRenderer.send('restart-to-install'),
});
