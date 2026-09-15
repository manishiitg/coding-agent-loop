---
name: work-integrations
description: Connect and manage Work MCP servers, secrets, browser access, models, and administrator-authorized server folders. Use when the user asks to connect a service, configure credentials or browser access, choose a coding provider, diagnose an integration, or attach an external folder.
---

# Work integrations

Inspect current state before changing it, and distinguish account-level setup
from selection for this project.

## MCP servers

- Use `list_mcp_servers` to inspect installed connection and authorization
  state. Use `search_mcp_catalog` only to discover a new server.
- Use `install_mcp_server` for a catalog result or service URL so Work can probe
  its authentication requirements. Use `add_mcp_server` only for a custom
  server whose protocol and complete configuration are already known.
- An installed server is account-level and is not automatically selected for
  this project. Project selection remains in **Setup > MCP servers**. Never
  claim the server's tools are available until it is connected and selected.
- Use `get_mcp_server_logs` to diagnose a configured server and
  `trigger_mcp_discovery` when its tool metadata is stale. Removing a server is
  account-wide, so identify the exact server and explain that scope first.

## Secrets

- Call `list_secrets` before creating, replacing, or deleting a secret.
- `set_workflow_secret` and `delete_workflow_secret` are legacy tool names for
  project-scoped secrets in Work. Use `set_user_secret` and
  `delete_user_secret` only when the user requests an account-level secret.
- After `set_workflow_secret` succeeds, `$SECRET_<NAME>` is available to shell
  tools immediately in the current chat and remains available in later turns.
  Continue the requested work in the same chat; do not ask the user to start a
  new chat or session. Verify availability without printing the secret value.
- Work stores attached secret names in `workflow.json` under
  `capabilities.selected_secrets`, using the AgentWorks workflow contract.
  Secret values remain encrypted outside the manifest. Respect the user's
  selections in **Setup > Secrets**; do not attach an unrelated credential.
- Never print, echo, store in project files, or otherwise reveal a secret
  value. Refer to secrets by name.

## Attached folders

- Use `list_work_folders` first. Attach only an existing path permitted by the
  administrator, with a clear alias and the least access needed (`read_only`
  unless writes are required).
- Use the exact stored ID when detaching. A newly attached folder becomes part
  of the native CLI guard on the next turn; do not pretend the current turn's
  process gained access retroactively.

## Browser and models

Browser mode and CDP settings live in the project's **Browser** view. Coding
provider and model settings live in **Setup > Models**: the provider is fixed
after the first message because it owns native conversation state, while the
model may still change. Use `agent-browser` for an actual browser task.
