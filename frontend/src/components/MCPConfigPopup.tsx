import React, { useState, useEffect } from 'react';
import { Loader2, Plus, Trash2, Code, X, Check, AlertCircle, Server, FileJson } from 'lucide-react';
import { mcpConfigApi } from '../services/mcpConfigApi';
import MCPConfigEditor from './MCPConfigEditor';

interface MCPServerConfig {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  protocol?: string;
  oauth?: {
    auto_discover?: boolean;
    use_pkce?: boolean;
    token_file?: string;
    auth_url?: string;
    token_url?: string;
    client_id?: string;
  };
}

interface MCPConfig {
  mcpServers: Record<string, MCPServerConfig>;
}

interface MCPConfigPopupProps {
  onClose: () => void;
  onConfigChange?: () => void;
}

export const MCPConfigPopup: React.FC<MCPConfigPopupProps> = ({
  onClose,
  onConfigChange
}) => {
  const [config, setConfig] = useState<MCPConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showJsonEditor, setShowJsonEditor] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [addMode, setAddMode] = useState<'form' | 'json'>('form');
  const [addJsonText, setAddJsonText] = useState('');
  const [addJsonError, setAddJsonError] = useState<string | null>(null);
  const [addFormData, setAddFormData] = useState({
    name: '',
    type: 'url' as 'url' | 'command',
    url: '',
    command: '',
    args: '',
    useOAuth: false
  });
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await mcpConfigApi.getConfig() as MCPConfig;
      setConfig(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load config');
    } finally {
      setLoading(false);
    }
  };

  const saveConfig = async (newConfig: MCPConfig) => {
    try {
      setSaving(true);
      setError(null);
      await mcpConfigApi.saveConfig(newConfig);
      setConfig(newConfig);
      onConfigChange?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save config');
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteServer = async (serverName: string) => {
    if (!config) return;

    const confirmed = window.confirm(`Are you sure you want to delete "${serverName}"?`);
    if (!confirmed) return;

    const newConfig = { ...config };
    delete newConfig.mcpServers[serverName];
    await saveConfig(newConfig);
  };

  const handleAddServer = async () => {
    if (!config || !addFormData.name.trim()) return;

    const newConfig = { ...config };
    const serverConfig: MCPServerConfig = {};

    if (addFormData.type === 'url') {
      serverConfig.url = addFormData.url;
      if (addFormData.useOAuth) {
        serverConfig.oauth = {
          auto_discover: true,
          use_pkce: true,
          token_file: `~/.config/mcpagent/tokens/${addFormData.name}.json`
        };
      }
    } else {
      serverConfig.command = addFormData.command;
      if (addFormData.args.trim()) {
        serverConfig.args = addFormData.args.split(' ').filter(Boolean);
      }
    }

    newConfig.mcpServers[addFormData.name] = serverConfig;
    await saveConfig(newConfig);

    // Reset form
    setAddFormData({
      name: '',
      type: 'url',
      url: '',
      command: '',
      args: '',
      useOAuth: false
    });
    setShowAddForm(false);
  };

  const handleAddServerFromJson = async () => {
    if (!config || !addJsonText.trim()) return;

    let parsed: Record<string, MCPServerConfig>;
    try {
      parsed = JSON.parse(addJsonText);
    } catch {
      setAddJsonError('Invalid JSON');
      return;
    }

    // Validate structure: must be an object with string keys
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      setAddJsonError('JSON must be an object mapping server names to their config');
      return;
    }

    const newConfig = { ...config, mcpServers: { ...config.mcpServers } };
    for (const [name, serverConfig] of Object.entries(parsed)) {
      newConfig.mcpServers[name] = serverConfig;
    }

    await saveConfig(newConfig);
    setAddJsonText('');
    setAddJsonError(null);
    setShowAddForm(false);
  };

  const handleJsonTextChange = (value: string) => {
    setAddJsonText(value);
    if (!value.trim()) {
      setAddJsonError(null);
      return;
    }
    try {
      const parsed = JSON.parse(value);
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        setAddJsonError('JSON must be an object mapping server names to their config');
      } else {
        setAddJsonError(null);
      }
    } catch {
      setAddJsonError('Invalid JSON');
    }
  };

  const getServerType = (serverConfig: MCPServerConfig): string => {
    if (serverConfig.url) return 'HTTP/SSE';
    if (serverConfig.command) return 'Stdio';
    return 'Unknown';
  };

  const getServerEndpoint = (serverConfig: MCPServerConfig): string => {
    if (serverConfig.url) return serverConfig.url;
    if (serverConfig.command) {
      const args = serverConfig.args?.join(' ') || '';
      return `${serverConfig.command} ${args}`.trim();
    }
    return 'N/A';
  };

  if (loading) {
    return (
      <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
        <div className="bg-white dark:bg-gray-800 rounded-lg p-8 flex items-center gap-3">
          <Loader2 className="w-6 h-6 animate-spin text-blue-500" />
          <span className="text-gray-700 dark:text-gray-300">Loading MCP configuration...</span>
        </div>
      </div>
    );
  }

  // Show JSON Editor view
  if (showJsonEditor) {
    return (
      <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-6xl h-[90vh] overflow-y-auto">
          <div className="flex items-center justify-between mb-4">
            <button
              onClick={() => setShowJsonEditor(false)}
              className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 flex items-center gap-2"
            >
              ← Back to Server List
            </button>
          </div>
          <MCPConfigEditor
            onConfigChange={() => {
              loadConfig();
              onConfigChange?.();
            }}
            onClose={() => setShowJsonEditor(false)}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <Server className="w-5 h-5 text-blue-500" />
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Configure MCP Servers
            </h2>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowJsonEditor(true)}
              className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md flex items-center gap-2 transition-colors"
            >
              <Code className="w-4 h-4" />
              Edit JSON
            </button>
            <button
              onClick={onClose}
              className="p-1.5 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Error Alert */}
        {error && (
          <div className="mx-4 mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md flex items-center gap-2">
            <AlertCircle className="w-4 h-4 text-red-500 flex-shrink-0" />
            <span className="text-sm text-red-700 dark:text-red-400">{error}</span>
          </div>
        )}

        {/* Server List */}
        <div className="flex-1 overflow-y-auto p-4">
          <div className="space-y-3">
            {config && Object.entries(config.mcpServers).map(([serverName, serverConfig]) => (
              <div
                key={serverName}
                className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg p-4"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className="w-2 h-2 rounded-full bg-green-500"></div>
                    <div>
                      <h3 className="font-medium text-gray-900 dark:text-gray-100">
                        {serverName}
                      </h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5 truncate max-w-md">
                        {getServerEndpoint(serverConfig)}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="px-2 py-1 text-xs font-medium rounded-full bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300">
                      {getServerType(serverConfig)}
                    </span>
                    {serverConfig.oauth && (
                      <span className="px-2 py-1 text-xs font-medium rounded-full bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300">
                        OAuth
                      </span>
                    )}
                    <button
                      onClick={() => handleDeleteServer(serverName)}
                      disabled={saving}
                      className="p-1.5 text-gray-400 hover:text-red-500 dark:hover:text-red-400 rounded transition-colors disabled:opacity-50"
                      title="Delete server"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>
            ))}

            {config && Object.keys(config.mcpServers).length === 0 && (
              <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                <Server className="w-12 h-12 mx-auto mb-3 opacity-50" />
                <p>No MCP servers configured</p>
                <p className="text-sm mt-1">Click "Add Server" to get started</p>
              </div>
            )}
          </div>

          {/* Add Server Form */}
          {showAddForm && (
            <div className="mt-4 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between mb-3">
                <h3 className="font-medium text-gray-900 dark:text-gray-100">Add New Server</h3>
                <div className="flex bg-gray-200 dark:bg-gray-700 rounded-md p-0.5">
                  <button
                    onClick={() => setAddMode('form')}
                    className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                      addMode === 'form'
                        ? 'bg-white dark:bg-gray-600 text-gray-900 dark:text-gray-100 shadow-sm'
                        : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200'
                    }`}
                  >
                    Form
                  </button>
                  <button
                    onClick={() => setAddMode('json')}
                    className={`px-3 py-1 text-xs font-medium rounded transition-colors flex items-center gap-1 ${
                      addMode === 'json'
                        ? 'bg-white dark:bg-gray-600 text-gray-900 dark:text-gray-100 shadow-sm'
                        : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200'
                    }`}
                  >
                    <FileJson className="w-3 h-3" />
                    JSON
                  </button>
                </div>
              </div>

              {addMode === 'form' ? (
                <div className="space-y-3">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Server Name
                    </label>
                    <input
                      type="text"
                      value={addFormData.name}
                      onChange={(e) => setAddFormData({ ...addFormData, name: e.target.value })}
                      placeholder="e.g., my-server"
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    />
                  </div>

                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Connection Type
                    </label>
                    <div className="flex gap-4">
                      <label className="flex items-center gap-2">
                        <input
                          type="radio"
                          checked={addFormData.type === 'url'}
                          onChange={() => setAddFormData({ ...addFormData, type: 'url' })}
                          className="text-blue-500"
                        />
                        <span className="text-sm text-gray-700 dark:text-gray-300">URL (HTTP/SSE)</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input
                          type="radio"
                          checked={addFormData.type === 'command'}
                          onChange={() => setAddFormData({ ...addFormData, type: 'command' })}
                          className="text-blue-500"
                        />
                        <span className="text-sm text-gray-700 dark:text-gray-300">Command (Stdio)</span>
                      </label>
                    </div>
                  </div>

                  {addFormData.type === 'url' ? (
                    <>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          Server URL
                        </label>
                        <input
                          type="text"
                          value={addFormData.url}
                          onChange={(e) => setAddFormData({ ...addFormData, url: e.target.value })}
                          placeholder="https://mcp.example.com/mcp"
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                        />
                      </div>
                      <div>
                        <label className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={addFormData.useOAuth}
                            onChange={(e) => setAddFormData({ ...addFormData, useOAuth: e.target.checked })}
                            className="text-blue-500 rounded"
                          />
                          <span className="text-sm text-gray-700 dark:text-gray-300">
                            Enable OAuth (auto-discover endpoints)
                          </span>
                        </label>
                      </div>
                    </>
                  ) : (
                    <>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          Command
                        </label>
                        <input
                          type="text"
                          value={addFormData.command}
                          onChange={(e) => setAddFormData({ ...addFormData, command: e.target.value })}
                          placeholder="e.g., npx"
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          Arguments (space-separated)
                        </label>
                        <input
                          type="text"
                          value={addFormData.args}
                          onChange={(e) => setAddFormData({ ...addFormData, args: e.target.value })}
                          placeholder="e.g., -y @my-mcp/server"
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                        />
                      </div>
                    </>
                  )}

                  <div className="flex justify-end gap-2 pt-2">
                    <button
                      onClick={() => setShowAddForm(false)}
                      className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
                    >
                      Cancel
                    </button>
                    <button
                      onClick={handleAddServer}
                      disabled={!addFormData.name.trim() || saving}
                      className="px-4 py-1.5 text-sm bg-blue-500 hover:bg-blue-600 text-white rounded-md flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                    >
                      {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
                      Add Server
                    </button>
                  </div>
                </div>
              ) : (
                <div className="space-y-3">
                  <p className="text-xs text-gray-500 dark:text-gray-400">
                    Paste server JSON. Each key is a server name, value is its config. Existing servers with the same name will be overwritten.
                  </p>
                  <textarea
                    value={addJsonText}
                    onChange={(e) => handleJsonTextChange(e.target.value)}
                    placeholder={`{\n  "my-server": {\n    "url": "https://mcp.example.com/sse"\n  },\n  "local-tool": {\n    "command": "npx",\n    "args": ["-y", "@my/mcp-server"]\n  }\n}`}
                    className={`w-full h-48 px-3 py-2 border rounded-md font-mono text-sm resize-y bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 ${
                      addJsonError
                        ? 'border-red-400 dark:border-red-600'
                        : 'border-gray-300 dark:border-gray-600'
                    }`}
                  />
                  {addJsonError && (
                    <div className="flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
                      <AlertCircle className="w-3 h-3 flex-shrink-0" />
                      {addJsonError}
                    </div>
                  )}
                  <div className="flex justify-end gap-2 pt-2">
                    <button
                      onClick={() => { setShowAddForm(false); setAddJsonText(''); setAddJsonError(null); }}
                      className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
                    >
                      Cancel
                    </button>
                    <button
                      onClick={handleAddServerFromJson}
                      disabled={!addJsonText.trim() || !!addJsonError || saving}
                      className="px-4 py-1.5 text-sm bg-blue-500 hover:bg-blue-600 text-white rounded-md flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                    >
                      {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
                      Add from JSON
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-4 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50">
          <div className="text-sm text-gray-500 dark:text-gray-400">
            {config ? Object.keys(config.mcpServers).length : 0} servers configured
          </div>
          <div className="flex items-center gap-2">
            {!showAddForm && (
              <button
                onClick={() => setShowAddForm(true)}
                className="px-4 py-2 text-sm bg-blue-500 hover:bg-blue-600 text-white rounded-md flex items-center gap-2 transition-colors"
              >
                <Plus className="w-4 h-4" />
                Add Server
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default MCPConfigPopup;
