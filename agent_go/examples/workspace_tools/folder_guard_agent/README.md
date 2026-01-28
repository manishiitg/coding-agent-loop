# Folder Guard Example

Shows how to use **folder guards** to restrict the agent to specific workspace paths: read-only from `docs/`, write-only to `output/`.

## What are folder guards?

`FolderGuardConfig` limits which paths the workspace client allows:

- **ReadPaths**: paths the agent can read (list, read file, etc.)
- **WritePaths**: paths the agent can write (create, update, delete)
- **BlockedPaths**: deny list (checked first)

Paths are workspace-relative (e.g. `docs`, `output`). The client validates every tool call; requests outside allowed paths fail before hitting the API.

## Prerequisites

- Go 1.22+, Docker & Docker Compose
- LLM API key (e.g. `OPENAI_API_KEY` or your provider’s key)

## Docker Compose & setup

Same as other examples: `manishiitg/workspace-api:latest`, port `8083`, volume `./workspace-docs`.

```bash
mkdir -p workspace-docs/docs workspace-docs/output
echo "Sample file" > workspace-docs/docs/sample.txt
docker-compose up -d
```

Set your LLM API key (and optional `WORKSPACE_API_URL` if not `http://localhost:8083`).

## Run

```bash
go run main.go
```

The agent is configured with:

- **ReadPaths**: `["docs"]` — can only list/read under `docs/`
- **WritePaths**: `["output"]` — can only create/update under `output/`

The prompt asks it to list files in `docs` and create `summary.txt` in `output`. Any attempt to read/write outside those paths returns an error from the client.

## Using folder guard in your code

1. Create a client with `WithFolderGuard`:

```go
folderGuard := &workspace.FolderGuardConfig{
    Enabled:    true,
    ReadPaths:  []string{"docs", "templates"},
    WritePaths: []string{"output"},
    BlockedPaths: []string{"secrets"}, // optional
}
wsClient := workspace.NewClient(workspaceAPIURL, workspace.WithFolderGuard(folderGuard))
```

2. Use the same client for all workspace tools (list, read, update, execute_shell_command, etc.). Path validation runs inside each call.

## Cleanup

```bash
docker-compose down
```
