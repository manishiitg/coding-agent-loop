# 📧 Google Workspace (GWS) Setup

MCP Agent Builder integrates with Google Workspace via the `gws` CLI. This allows agents to interact with Gmail, Drive, Calendar, and more.

## 🚀 Quick Setup

### 1. Authenticate
On your local machine, run the following command to perform the initial OAuth handshake:

```bash
gws auth login --client-secret client_secret.json
```
*This will open your browser. Grant the necessary permissions to finish the login.*

### 2. Export Credentials
Extract the unmasked authentication token into the orchestrator's directory:

```bash
# Export from your local gws config to the project folder
gws auth export --unmasked > agent_go/gws-credentials.json
```
**CRITICAL**: The file MUST be named `gws-credentials.json` and placed in the `agent_go/` folder for the Docker container to pick it up.

### 3. Sync & Enable in UI
1. Open the UI (`http://localhost:5173`) and go to **Settings > Google Workspace**.
2. Click **Sync Skills from GitHub** to load the tool definitions.
3. Toggle **Enable Google Workspace** for your agent runs.

---

## 🤖 Capabilities
Once set up, your agent can execute commands like:
*   `gws gmail messages list`
*   `gws drive files export <id>`
*   `gws sheets spreadsheets.values get <id>`

For full details on using the CLI, see the **[GWS CLI Documentation](https://github.com/googleworkspace/cli)**.
