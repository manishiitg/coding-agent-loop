---
name: work-mcp
description: Connect and manage MCP servers for Crew projects. Use when the user asks to connect a service through MCP, inspect connection or authorization state, select MCP access for a project, refresh tool discovery, or diagnose an MCP server.
---

# Crew MCP

Inspect the current MCP state before changing it, and distinguish platform-level
connection setup from selection for this project.

- Use `list_mcp_servers` to inspect installed connection and authorization
  state. Use `search_mcp_catalog` only to discover a new server.
- Use `install_mcp_server` for a catalog result or service URL so Crew can probe
  its authentication requirements. Use `add_mcp_server` only for a custom
  server whose protocol and complete configuration are already known.
- An installed server is a platform connection shared by AgentWorks, Crew,
  workflows, chats, schedules, and every user. Only a platform administrator
  may add, authenticate, reconnect, edit, or remove one. Explain this before
  starting credential or OAuth setup. A connection is not automatically
  selected for this project.
- When the user asks to use an already-connected server in the active Crew
  project, call `update_project_mcp_server_selection` with `action: select`.
  The selection is written to this project's `workflow.json`; its tools become
  available on the next user message because the current turn retains its
  original MCP scope. Do not tell the user to open Setup when this tool can do
  the selection. Use `action: deselect` when the user asks to remove project
  access. The same selection remains editable in **Setup > MCP servers**.
- Never claim server tools are available until the server is connected and
  selected. After selecting, clearly state the next-message boundary.
- Use `get_mcp_server_logs` to diagnose a configured server and
  `trigger_mcp_discovery` when its tool metadata is stale. Removing a server is
  platform-wide, so identify the exact server and explain that scope first.
