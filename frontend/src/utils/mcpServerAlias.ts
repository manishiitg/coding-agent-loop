// PLAT-169: hyphen and underscore MCP server names are compatibility aliases
// throughout MCP resolution — mirrors the backend's own normalization in
// ValidateManifest (agent_go/cmd/server/workflow_manifest.go). A manifest
// saved under a legacy spelling (e.g. "google_sheets") must still be
// recognized as the same server as the current catalog's canonical spelling
// (e.g. "google-sheets"), or a UI comparing them by exact string produces a
// checkbox that looks unchecked when the server IS already selected —
// leading a user to "select" it again and create a duplicate the backend
// then permanently rejects.

export const normalizeServerAlias = (name: string) => name.trim().replace(/_/g, '-');

export const serverNamesMatch = (a: string, b: string) => normalizeServerAlias(a) === normalizeServerAlias(b);

export const isSelectedServer = (selectedServers: string[], serverName: string) =>
  selectedServers.some(s => serverNamesMatch(s, serverName));

// A "server:tool" entry belongs to serverName if its own prefix (before the
// first ":") is an alias match — not just an exact string match — so
// clearing a server's tools also clears any legacy-spelled entries for it.
export const toolBelongsToServer = (tool: string, serverName: string) => {
  const separatorIndex = tool.indexOf(':');
  if (separatorIndex === -1) return false;
  return serverNamesMatch(tool.slice(0, separatorIndex), serverName);
};

// Alias-aware equivalent of `tools.includes(`${serverName}:${toolName}`)`.
export const hasServerTool = (tools: string[], serverName: string, toolName: string) => {
  return tools.some(t => {
    const separatorIndex = t.indexOf(':');
    if (separatorIndex === -1) return false;
    return serverNamesMatch(t.slice(0, separatorIndex), serverName) && t.slice(separatorIndex + 1) === toolName;
  });
};

// Shared by ToolSelectionSection's own checkbox and any other UI that lets a
// user add/remove a server from a workflow's tool list (e.g. the "Connect a
// new MCP server" browser embedded in the workflow panel) — keeps the
// alias-safety and default-tool-mode behavior identical no matter which
// surface triggered the toggle.
export const toggleServerSelection = (
  serverName: string,
  selectedServers: string[],
  selectedTools: string[],
): { servers: string[]; tools: string[] } => {
  if (isSelectedServer(selectedServers, serverName)) {
    return {
      servers: selectedServers.filter(s => !serverNamesMatch(s, serverName)),
      tools: selectedTools.filter(t => !toolBelongsToServer(t, serverName)),
    };
  }

  const specificTools = selectedTools.filter(t => toolBelongsToServer(t, serverName) && !t.endsWith(':*'));
  const servers = [...selectedServers.filter(s => !serverNamesMatch(s, serverName)), serverName];
  // A previously removed server may still have stale wildcard markers in the
  // manifest. Restore a single marker instead of accumulating duplicates.
  const otherTools = selectedTools.filter(t => !toolBelongsToServer(t, serverName));
  const tools = specificTools.length > 0 ? [...otherTools, ...new Set(specificTools)] : [...otherTools, `${serverName}:*`];
  return { servers, tools };
};

// dedupeServerNames collapses alias-equivalent duplicates to a single entry
// (first occurrence wins), so a manifest that already has both spellings —
// e.g. from before this alias-awareness existed — self-heals the next time
// it is saved, instead of permanently failing the backend's duplicate
// validation until someone hand-edits the JSON.
export const dedupeServerNames = (servers: string[]): string[] => {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const server of servers) {
    const key = normalizeServerAlias(server);
    if (seen.has(key)) continue;
    seen.add(key);
    result.push(server);
  }
  return result;
};
