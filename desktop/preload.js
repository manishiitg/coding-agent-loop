const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
  getApiBaseUrl: () => {
    // If dev mode (port 5173), point to 8000
    if (window.location.port === '5173') return 'http://localhost:8000';
    // Otherwise, use the port the window loaded from (dynamic agent port)
    return window.location.origin;
  },
  getWorkspaceApiBaseUrl: () => {
    // Get the workspace port from main process (sync)
    const port = ipcRenderer.sendSync('get-workspace-port');
    return `http://127.0.0.1:${port}`;
  },
  setDockBadge: (text) => ipcRenderer.send('set-dock-badge', text),
  openExternal: (url) => ipcRenderer.send('open-external', url),
  printToPDF: (filename) => ipcRenderer.invoke('print-to-pdf', filename),
});
