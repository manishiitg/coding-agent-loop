const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
  getApiBaseUrl: () => 'http://127.0.0.1:45678',
  getWorkspaceApiBaseUrl: () => 'http://127.0.0.1:45679',
  setDockBadge: (text) => ipcRenderer.send('set-dock-badge', text)
});
