# Agent with Workspace Tools Example

This example demonstrates how to build an agent that utilizes the `workspace:basic` toolset from the `mcp-agent-builder-go/pkg` library.

## Prerequisites

- Go 1.21+
- Docker and Docker Compose

## Setup

1.  **Start the Workspace Service**
    The agent requires a backend service to perform file operations. We use the pre-built `workspace-api` image.

    ```bash
    docker-compose up -d
    ```

    This will start the workspace API at `http://localhost:8080`.

2.  **Run the Agent**
    
    ```bash
    go run main.go
    ```

## What it does

1.  Initializes a `workspace.Client` pointing to the local API.
2.  Retrieves `basic` tool definitions (`list`, `read`, `update`, etc.) via `workspace.GetBasicToolDefinitions()`.
3.  Creates an executor map via `workspace.NewBasicExecutor(client)`.
4.  Simulates an agent executing the `list_workspace_files` tool.

## Cleanup

Stop the background services:

```bash
docker-compose down
```
