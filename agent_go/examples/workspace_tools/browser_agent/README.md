# Browser Agent Example

Multimodal agent that uses **Shell**, **Image**, **PDF**, and **Browser** tools (shell commands, web browsing, screenshots, image/PDF analysis).

## Prerequisites

- Go 1.22+, Docker & Docker Compose
- An LLM provider API key (multiple providers supported). Set the appropriate key for your provider (e.g. `OPENAI_API_KEY`, or your provider’s key); no gcloud project or `gcloud auth` required.

## Docker Compose & Image

`docker-compose.yml` runs the **workspace API**:

- **Image**: `manishiitg/workspace-api:latest` (Docker Hub). Provides file ops, shell execution, image/PDF processing.
- **Port**: `8083:8080` → API at `http://localhost:8083`
- **Volume**: `./workspace-docs` → `/app/workspace-docs` (persistent files/screenshots)

## Setup

```bash
mkdir -p workspace-docs
docker-compose up -d
```

Set env (or use `.env`). Main requirement is your LLM provider’s API key, e.g.:

```bash
export OPENAI_API_KEY="your-key"   # or your provider’s key variable
# If using Vertex: GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION, and credentials as needed
```

## Run

```bash
go run main.go
```

The agent connects to the workspace API, loads shell/image/browser tools, and runs the demo (e.g. `ls -la`, visit example.com, screenshot, describe with `read_image`). Tool list is built from `workspace.GetShellToolDefinitions()`, `GetImageToolDefinitions()`, and `browser.GetToolDefinition()` — add or remove as needed.

## Cleanup

```bash
docker-compose down
```
