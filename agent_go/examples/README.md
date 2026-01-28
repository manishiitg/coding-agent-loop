# Examples

Go agents that use the workspace API and optional browser/LLM tooling. Each example runs an agent locally and talks to a workspace API service (Docker).

## Prerequisites (all examples)

- **Go 1.22+**
- **Docker & Docker Compose**
- **LLM API key** for your provider (e.g. `OPENAI_API_KEY`). Multiple LLM providers supported; no gcloud project or `gcloud auth` required for key-based auth.

## Workspace API (shared)

All examples expect the **workspace API** to be running:

- **Image**: `manishiitg/workspace-api:latest`
- **Port**: host `8083` → container `8080` → `http://localhost:8083`
- **Volume**: a folder (e.g. `./workspace-docs` or `./test-docs`) is mounted at `/app/workspace-docs` for persistent files.

Start it from inside an example directory:

```bash
docker-compose up -d
```

## Examples overview

| Example | Tools | Description |
|--------|--------|-------------|
| **[basic_agent](workspace_tools/basic_agent/)** | Basic file (list, read, write) | Lists workspace files and describes them; uses `workspace.GetBasicToolDefinitions()`. |
| **[agent_with_tools](workspace_tools/agent_with_tools/)** | Basic file | Creates dummy files and lists them; same basic tools, different prompt. |
| **[browser_agent](workspace_tools/browser_agent/)** | Shell, image, PDF, browser | Multimodal: shell, browse, screenshot, read image/PDF; uses granular tool getters. |

Each subfolder has its own `README.md`, `docker-compose.yml`, and `main.go`.

## Quick start

1. Pick an example and `cd` into it (e.g. `cd workspace_tools/browser_agent`).
2. Set your LLM API key (and any provider-specific env; see the example’s README).
3. Start the workspace API: `docker-compose up -d`.
4. Run the agent: `go run main.go`.
5. Stop the API: `docker-compose down`.

For detailed setup, env vars, and behavior, see the README in each example directory.
