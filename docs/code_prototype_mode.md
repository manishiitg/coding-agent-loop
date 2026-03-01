# Code Prototype Mode

A dedicated mode for building and deploying small web projects with AI assistance. The AI agent writes code directly into a per-user workspace folder; projects can be previewed live in the browser and deployed to Kubernetes, Vercel, or Railway in one click.

## Overview

Code Prototype mode sits alongside Chat, Workflow, and Multi-Agent modes. Selecting it opens a layout with a project header bar and a full-width chat area. The AI agent runs in `simple` delegation mode with delegation spawn enabled — identical to Multi-Agent mode — so it can spawn sub-agents for focused tasks like writing a specific file or running a build command.

Users describe what they want ("Add a counter button", "Add a `/health` endpoint") and the agent writes code into the project folder via workspace file tools. The right-side Workspace panel shows the live file tree. The preview proxy serves the Vite dev server running inside a per-project Docker container. Deployment opens the URL in a new browser tab.

---

## Project Structure

Each project lives under the user's workspace:

```
Projects/{projectName}/
├── .prototype.json            ← metadata, config, deploy history
├── memories/                  ← per-project agent memories (see Memory section)
├── docs/                      ← per-project docs maintained by the agent
├── bugs/                      ← known bugs and issues
├── plans/                     ← agent plan files
├── frontend/                  ← React 19 + Vite 7 + TypeScript 5.9
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── tsconfig.app.json
│   ├── tsconfig.node.json
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       └── App.tsx
└── backend/                   ← Express 5 + TypeScript 5.9
    ├── package.json
    ├── tsconfig.json
    └── src/
        └── index.ts
```

Project type controls which sub-folders are scaffolded:

| Type | frontend/ | backend/ |
|------|-----------|---------|
| `frontend-only` | yes | no |
| `backend-only` | no | yes |
| `fullstack` | yes | yes |

The `Projects/` folder is in `PerUserFolders` (`agent_go/pkg/common/types.go`), which means the workspace folder guard automatically grants the agent write access to `_users/{userID}/Projects/` without additional configuration.

---

## `.prototype.json` Schema

```json
{
  "name": "my-app",
  "type": "fullstack",
  "description": "Optional description",
  "created_at": "2026-02-28T00:00:00Z",
  "config": {
    "selected_servers": ["filesystem", "fetch"],
    "selected_secrets": ["OPENAI_API_KEY"],
    "selected_skills": ["code-review"],
    "selected_subagents": ["code-writer", "code-tester"],
    "llm_config": { "model": "claude-sonnet-4-6", "provider": "anthropic" }
  },
  "deployments": [
    {
      "id": "dpl_abc123",
      "provider": "vercel",
      "url": "https://my-app.vercel.app",
      "timestamp": "2026-02-28T01:00:00Z",
      "status": "success"
    }
  ]
}
```

The `config` block mirrors `ChatSessionConfig` fields and is applied to the chat tab each time the project is opened (see Agent Context section).

---

## Backend API

All routes are under `/api/code-prototype/` and require authentication (`X-User-ID` header via session cookie).

| Method | Path | Description |
|--------|------|-------------|
| GET | `/projects` | List all user's projects |
| POST | `/projects` | Scaffold project files + write `.prototype.json` |
| GET | `/projects/{name}` | Read `.prototype.json` for one project |
| PATCH | `/projects/{name}/config` | Update `config` block |
| DELETE | `/projects/{name}` | Delete project folder |
| POST | `/deploy` | Build + deploy, return logs + URL |
| DELETE | `/deploy/{projectName}` | Tear down K8s resources |
| POST | `/stop-dev` | Stop + remove all per-project Docker containers for this user |
| GET/ANY | `/preview/{name}/...` | Reverse-proxy to the per-project dev server container |

**File:** `agent_go/cmd/server/code_prototype_routes.go`
**Registration:** `server.go` → `RegisterCodePrototypeRoutes(apiRouter, api)`

### Project creation

`POST /projects` writes in-memory Go template files for the selected project type (frontend, backend, or both), then writes `.prototype.json` with the provided name, type, description, and config. All files are written through the workspace REST API (`PUT /api/documents/{path}`) using the same `X-User-ID` forwarding pattern used elsewhere in `server.go`.

### Project listing

`GET /projects` reads the workspace directory `Projects/` and returns one entry per sub-folder that contains a valid `.prototype.json`. The workspace API returns `{"data": [{"filepath": "...", "type": "folder"}]}`.

---

## Dev Server — Per-Project Docker Containers

Each project gets its own isolated Docker container (DooD — Docker outside of Docker). The agent process mounts `/var/run/docker.sock` and uses `docker` CLI to create sibling containers on the host, rather than running dev servers inside the shared `workspace-api` container.

### Architecture

```
Browser → Go server (:8000) → /api/code-prototype/preview/{name}/
  → handlePreviewProxy
    → getProjectContainerURL → docker inspect prototype-{userID}-{name}
    → httputil.ReverseProxy → http://localhost:{hashPort} (or http://{containerName}:{hashPort} in Docker mode)
    → Vite dev server (node:20-alpine container)
```

### Container naming

```
prototype-{userID}-{projectName}
```

Examples: `prototype-alice-my-app`, `prototype-default-test2`

### Port assignment

Port is deterministic — FNV-32a hash of the project name, range 15000–15099:

```go
func projectDevPort(projectName string) int {
    h := fnv.New32a()
    h.Write([]byte(projectName))
    return 15000 + int(h.Sum32()%100)
}
```

The container maps `-p {port}:{port}` and Vite is configured with `port: {hashPort}` in `vite.config.ts`.

### Container lifecycle

1. **First request to preview** → `getProjectContainerURL` checks `docker inspect` → container missing or not running → show spinner, fire `go startProjectContainer(...)`.
2. **`startProjectContainer`** checks container status via `docker inspect --format {{.State.Status}}`:
   - `running` → return (already up)
   - `exited` / `created` / `paused` → `docker start {name}` (fast restart, node_modules preserved)
   - anything else (no such container) → `docker run -d ...` (first run, triggers npm install)
3. **Subsequent requests** → `docker inspect` returns `true` → proxy connects immediately.
4. **Agent restart** → containers survive (`--restart unless-stopped`).
5. **`POST /stop-dev`** → `docker stop` + `docker rm` + `docker volume rm` for all `prototype-{userID}-*` containers.

### docker run arguments

```
docker run -d
  --name prototype-{userID}-{name}
  --restart unless-stopped
  -p {hashPort}:{hashPort}
  [--network {DOCKER_NETWORK}]
  -v {WORKSPACE_DOCS_HOST_PATH}/_users/{userID}/Projects/{name}/frontend:/app/frontend
  [-v {…}/backend:/app/backend   (fullstack / backend-only)]
  -v prototype-{userID}-{name}-fe-modules:/app/frontend/node_modules
  [-v prototype-{userID}-{name}-be-modules:/app/backend/node_modules]
  -e VITE_BASE=/api/code-prototype/preview/{name}/
  [-e PORT=8080   (fullstack / backend-only)]
  node:20-alpine
  sh -c "{startCmd}"
```

Start commands by project type:

| Type | Command |
|------|---------|
| `frontend-only` | `npm install --prefix /app/frontend && VITE_BASE=$VITE_BASE npm run dev --prefix /app/frontend` |
| `backend-only` | `npm install --prefix /app/backend && npm run dev --prefix /app/backend` |
| `fullstack` | `npm install --prefix /app/frontend && npm install --prefix /app/backend && (VITE_BASE=$VITE_BASE npm run dev --prefix /app/frontend &) && (npm run dev --prefix /app/backend &) && wait` |

### Named volumes for node_modules

Each container uses named Docker volumes for `node_modules` so that npm install is preserved across container restarts:

- `prototype-{userID}-{name}-fe-modules` → `/app/frontend/node_modules`
- `prototype-{userID}-{name}-be-modules` → `/app/backend/node_modules`

These volumes are removed by `POST /stop-dev`.

### Throttle: containerStartOnce

`containerStarting sync.Map` (key: `"userID/projectName"`) records the last start attempt timestamp. A new `docker run` or `docker start` is only fired if no attempt was made in the last 3 minutes — prevents hammering docker on every 5-second spinner auto-refresh.

### Debugging dev servers

```bash
# See Vite/npm output for a project
docker logs prototype-{userID}-{projectName}

# Follow logs live
docker logs -f prototype-default-my-app

# List all running project containers
docker ps --filter "name=prototype-"

# Inspect a container
docker inspect prototype-default-my-app
```

### WebSocket (Vite HMR)

`isWebSocketUpgrade` detects WebSocket upgrade requests. These are forwarded via `proxyWebSocketToDevServer` which dials the container's backend WebSocket directly, bridging the browser ↔ Vite HMR connection for live reload.

---

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `WORKSPACE_DOCS_HOST_PATH` | `./workspace-docs` (abs) | **Host-machine** absolute path of workspace-docs dir. Must be set in Docker Compose so `docker run -v` bind-mounts use the correct host path. |
| `DOCKER_NETWORK` | *(empty)* | If set, project containers join this network and are reached by container name instead of `localhost`. Set to `{COMPOSE_PROJECT_NAME}_default` in prod. |
| `PROTOTYPE_CONTAINER_IMAGE` | `node:20-alpine` | Override the Node.js image for project containers. |

**Important:** `WORKSPACE_DOCS_HOST_PATH` must be the **host machine's** path, not the container-internal path. For example, if the agent container mounts `./workspace-docs:/app/workspace-docs`, set `WORKSPACE_DOCS_HOST_PATH=/absolute/path/to/workspace-docs` on the host.

---

## Docker Compose Configuration

### `docker-compose.yml`

`workspace-api` no longer needs the `15000-15099` port range — project containers map their own ports directly to the host:

```yaml
workspace-api:
  ports:
    - "8081:8080"
    # Project containers bind their own ports via docker run -p {port}:{port}
```

### `docker-compose.prod.yml`

The agent service requires the Docker socket and path env vars:

```yaml
agent:
  environment:
    - WORKSPACE_API_URL=http://workspace-api:8080
    - WORKSPACE_DOCS_HOST_PATH=${WORKSPACE_DOCS_HOST_PATH:-/app/workspace-docs-host}
    - DOCKER_NETWORK=${COMPOSE_PROJECT_NAME:-mcp-agent-builder-go}_default
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock
    - ${WORKSPACE_DOCS_HOST_PATH:-./workspace-docs}:/app/workspace-docs-host:ro
```

Set `WORKSPACE_DOCS_HOST_PATH` to the **host absolute path** before deploying:

```bash
export WORKSPACE_DOCS_HOST_PATH=/data/workspace-docs
docker compose -f docker-compose.yml -f docker-compose.prod.yml up --build
```

---

## Frontend Components

**Directory:** `frontend/src/components/code-prototype/`

| File | Purpose |
|------|---------|
| `CodePrototypeLayout.tsx` | Top-level layout: header + ChatArea + DeployDrawer |
| `CodePrototypeHeader.tsx` | Project selector, ⚙ Config button, provider dropdown, Deploy button |
| `DeployDrawer.tsx` | Slide-up sheet with terminal-style deploy logs and URL link |
| `NewProjectWizard.tsx` | Dialog: name, type, MCP servers, secrets, skills, sub-agents |
| `ProjectListModal.tsx` | Dialog listing all projects with switch/delete actions |
| `ProjectConfigModal.tsx` | Dialog for editing an existing project's config |

**Store:** `frontend/src/stores/useCodePrototypeStore.ts`

Persisted to localStorage: `currentProject`, `selectedProvider`.

State includes: `currentProject`, `projectList`, `isDeploying`, `deployOutput`, `lastDeployedUrl`, `selectedProvider`, `showNewProjectWizard`, `showProjectList`.

**API module:** `frontend/src/services/api.ts` → `agentApi` methods:
- `codePrototype.listProjects()`, `createProject()`, `getProject()`, `deleteProject()`, `deploy()`

**Mode wiring:** `frontend/src/App.tsx`

```tsx
{/* Code Prototype mode */}
<div className={selectedModeCategory === 'code-prototype' ? 'h-full' : 'hidden'}>
  <CodePrototypeLayout onNewChat={startNewChat} />
</div>
```

**Submission fix:** `frontend/src/utils/chatSubmitHelpers.ts`

```typescript
const isMultiAgentMode =
  selectedModeCategory === 'multi-agent' || selectedModeCategory === 'code-prototype'
```

This single flag cascades to: `delegation_mode: 'plan'`, `plan_phase`, `delegation_tier_config`, `enable_workspace_access`, `selected_skills`, `selected_subagents`, and context summarization — all of which are required for the agent to work correctly in prototype mode.

---

## Agent Context Injection

When a project is selected (or restored from localStorage), `CodePrototypeLayout` injects LLM guidance into the active session via `agentApi.setLLMGuidance(sessionId, guidance, memoryFolder)`.

**What the guidance contains:**

1. Project name and root path (`Projects/{projectName}/`)
2. Stack info (React 19 + Vite 7 frontend, Express 5 backend)
3. Canonical file path examples
4. Build, dev, and install commands per project type
5. Role instructions (use workspace tools, read before modifying, delegate focused tasks)
6. Coding guidelines (TypeScript, React hooks, async/await)
7. Documentation and memory conventions

**Project config is applied to the chat tab** before guidance injection:

```typescript
useChatStore.getState().setTabConfig(activeTabId, {
  selectedServers:   currentProject.config.selected_servers ?? [],
  selectedSecrets:   currentProject.config.selected_secrets ?? [],
  selectedSkills:    currentProject.config.selected_skills ?? [],
  selectedSubAgents: currentProject.config.selected_subagents ?? [],
})
```

---

## Memory System

Code Prototype mode uses **per-project memories** rather than the shared `Plans/memories/` folder used by multi-agent chat.

### Storage location

| Mode | Memory folder |
|------|--------------|
| Multi-Agent Chat | `Plans/memories/YYYY-MM/` |
| Code Prototype | `Projects/{projectName}/memories/YYYY-MM/` |

### How it works

1. `CodePrototypeLayout` calls `agentApi.setLLMGuidance(sessionId, guidance, memoryFolder)` where `memoryFolder = "Projects/${name}/memories"`.
2. The backend `handleSetLLMGuidance` stores `MemoryFolder` on `ActiveSessionInfo` (in the in-memory sessions map).
3. On each memory tool invocation, `wrappedMemoryExecutor` in `server.go` looks up the session's `MemoryFolder` and injects it into the request context via `virtualtools.MemoryFolderKey`.
4. The memory handlers (`handleSaveMemory`, `handleRecallMemory`, `handleCompressMemory`) call `getMemoryFolder(ctx)` which reads the context key, falling back to `"Plans/memories"` if absent.

**Key files:**
- `agent_go/cmd/server/virtual-tools/memory_tools.go` — `MemoryFolderKey`, `getMemoryFolder`, all three handlers
- `agent_go/cmd/server/server.go` — `ActiveSessionInfo.MemoryFolder`, `wrappedMemoryExecutor`, memory workspace client
- `frontend/src/services/api-types.ts` — `LLMGuidanceRequest.memory_folder`
- `frontend/src/services/api.ts` — `setLLMGuidance(sessionId, guidance, memoryFolder?)`

### Workspace write paths

The memory workspace client is configured with:
```go
WritePaths: []string{"Plans", "Projects"}
```

This ensures the folder guard allows memory writes to both the standard `Plans/memories/` path and any `Projects/*/memories/` path.

---

## Deployment

### Provider: Kubernetes (primary)

The workspace-docs PVC is already mounted in-cluster. Prototype pods mount the same PVC and run code directly — no Docker image builds needed.

**Steps:**
1. Build via workspace `execute_shell_command`:
   - Frontend: `npm install && npm run build` in `Projects/{name}/frontend/`
   - Backend: `npm install && npm run build` in `Projects/{name}/backend/`
2. Generate K8s YAML from in-memory Go templates
3. `kubectl apply -f -` (or direct K8s API call)
4. Poll pod readiness (up to 60s)
5. Return URL: `https://{host}/prototypes/{projectName}/`

**K8s resources per prototype** (namespace: `prod-mcpagent`, label: `app.kubernetes.io/managed-by: mcpagent-prototype`):

- Frontend: `Deployment` (nginx serving `frontend/dist`), `ConfigMap` (nginx.conf), `Service`, `Ingress`
- Backend: `Deployment` (node running `dist/index.js`), `Service`, `Ingress`
- User secrets injected from K8s secret `prototype-{projectName}-secrets`

**RBAC:** `deploy/k8s/prototypes/rbac.yaml` — `Role` + `RoleBinding` scoped to `prod-mcpagent`, allowing create/update/delete of `deployments`, `services`, `ingresses`, `configmaps`, `secrets` with the managed-by label.

### Provider: Vercel (SaaS frontend)

1. Build frontend
2. POST to Vercel Files API with `dist/` contents
3. Auth: `VERCEL_TOKEN` from user secrets

### Provider: Railway (SaaS backend)

1. `railway up --service {projectName} --detach` via workspace execute
2. Auth: `RAILWAY_TOKEN` env var
3. Parse URL from stdout

### Common final steps

All providers append a `DeploymentRecord` to `.prototype.json` and return `{ url, logs, deploymentId }` to the frontend. The `DeployDrawer` displays log lines in a terminal-style view and shows an "Open ↗" link on success. `lastDeployedUrl` is persisted in the store for display in the header between sessions.

---

## Folder Guard

The `Projects/` root is included in `PerUserFolders` in `agent_go/pkg/common/types.go`:

```go
var PerUserFolders = []string{"Chats", "Downloads", "Plans", "Projects"}
```

The workspace folder guard (`wrapExecutorsWithChatModeFolderGuard`) restricts agent writes to `_users/{userID}/{folder}/` for each folder in this list. Adding `"Projects"` means all code-prototype agents automatically have write access to `_users/{userID}/Projects/` without any additional configuration.

---

## Multi-User Isolation

Each user's projects live under `_users/{userID}/Projects/`. All backend routes extract the user ID from the authenticated session, so user A's projects are never visible to user B. The workspace API enforces the same isolation at the file layer.

Per-project Docker containers are also user-scoped: `prototype-{userID}-{name}`. `POST /stop-dev` only stops containers whose name has the authenticated user's ID as prefix.

---

## New Project Wizard

`NewProjectWizard.tsx` collects:

1. **Project name** — slug-validated (lowercase letters, numbers, hyphens)
2. **Type** — Frontend only / Backend only / Fullstack (button group)
3. **MCP servers** — multi-select from available servers
4. **Secrets** — multi-select from user's stored secrets
5. **Skills** — multi-select from available skills
6. **Sub-agents** — checkbox list loaded from `subagentsApi.listSubAgents()`

On confirm: `codePrototypeApi.createProject()` → sets `currentProject` in store → wizard closes → `CodePrototypeLayout` applies config to tab and injects LLM guidance.

---

## Verification Checklist

1. Select "Code Prototype" in mode modal — layout renders with empty state / wizard prompt
2. Create a "fullstack" project with selected MCPs and a secret → `Projects/{name}/.prototype.json` written, scaffold files created
3. Open right-side Workspace panel → `Projects/{name}/` appears with scaffold files
4. Open preview URL → spinner page appears (container starting); refreshes automatically every 5s
5. After ~30s (npm install): preview shows the React app
6. `docker ps | grep prototype-default-{name}` → container is running
7. `docker logs prototype-default-{name}` → Vite startup output visible
8. Edit `Projects/{name}/frontend/src/App.tsx` → Vite HMR updates the preview in the browser
9. Chat: "Add a counter button to the frontend" → agent edits the file → HMR reloads preview
10. Click ⚙ Config → change selected servers → saved to `.prototype.json`, applied on next session start
11. **Stop dev:** `POST /api/code-prototype/stop-dev` → `docker ps` shows no `prototype-{userID}` containers; named volumes removed
12. **Agent restart:** containers re-appear on next preview request (docker start, fast path — node_modules intact)
13. **K8s deploy:** Select Kubernetes → deploy → `DeployDrawer` shows build + kubectl logs → URL appears, opens in new tab
14. Check K8s resources: `kubectl get deploy,svc,ingress -n prod-mcpagent -l app.kubernetes.io/managed-by=mcpagent-prototype`
15. **Memory isolation:** Save a memory in project A → verify it appears in `Projects/A/memories/`, not in `Plans/memories/` or `Projects/B/memories/`
16. **Multi-user:** user A's projects not visible to user B; `docker stop` only affects their own containers
