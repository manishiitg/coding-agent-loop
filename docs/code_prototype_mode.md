# Code Prototype Mode

A dedicated mode for building and deploying small web projects with AI assistance. The AI agent writes code directly into a per-user workspace folder; projects can be deployed to Kubernetes, Vercel, or Railway in one click.

## Overview

Code Prototype mode sits alongside Chat, Workflow, and Multi-Agent modes. Selecting it opens a layout with a project header bar and a full-width chat area. The AI agent runs in `simple` delegation mode with delegation spawn enabled — identical to Multi-Agent mode — so it can spawn sub-agents for focused tasks like writing a specific file or running a build command.

Users describe what they want ("Add a counter button", "Add a `/health` endpoint") and the agent writes code into the project folder via workspace file tools. The right-side Workspace panel shows the live file tree. Deployment opens the URL in a new browser tab.

---

## Project Structure

Each project lives under the user's workspace:

```
Projects/{projectName}/
├── .prototype.json            ← metadata, config, deploy history
├── memories/                  ← per-project agent memories (see Memory section)
├── frontend/                  ← React 18 + Vite 5 + TypeScript
│   ├── package.json
│   ├── vite.config.ts
│   ├── index.html
│   ├── tsconfig.json
│   └── src/
│       ├── main.tsx
│       └── App.tsx
└── backend/                   ← Express 4 + TypeScript
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

**File:** `agent_go/cmd/server/code_prototype_routes.go`
**Registration:** `server.go` → `RegisterCodePrototypeRoutes(apiRouter, api)`

### Project creation

`POST /projects` writes in-memory Go template files for the selected project type (frontend, backend, or both), then writes `.prototype.json` with the provided name, type, description, and config. All files are written through the workspace REST API (`PUT /api/documents/{path}`) using the same `X-User-ID` forwarding pattern used elsewhere in `server.go`.

### Project listing

`GET /projects` reads the workspace directory `Projects/` and returns one entry per sub-folder that contains a valid `.prototype.json`. The workspace API returns `{"data": [{"filepath": "...", "type": "folder"}]}`.

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
2. Stack info (React/Vite frontend, Express backend)
3. Setup status — shallow file tree (depth=2) checks for `node_modules` and `dist` existence only; the agent discovers the actual file structure itself
4. Canonical file path examples
5. Role instructions (use workspace tools, read before modifying, delegate focused tasks)
6. Coding guidelines (TypeScript, React hooks, async/await)

**Project config is applied to the chat tab** before guidance injection:

```typescript
useChatStore.getState().setTabConfig(activeTabId, {
  selectedServers:   currentProject.config.selected_servers ?? [],
  selectedSecrets:   currentProject.config.selected_secrets ?? [],
  selectedSkills:    currentProject.config.selected_skills ?? [],
  selectedSubAgents: currentProject.config.selected_subagents ?? [],
})
```

This mirrors exactly how preset queries apply their saved config in the existing workflow system.

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
4. Chat: "Add a counter button to the frontend" → agent edits `frontend/src/App.tsx` via workspace tools
5. Click ⚙ Config → change selected servers → saved to `.prototype.json`, applied on next session start
6. **K8s deploy:** Select Kubernetes → deploy → `DeployDrawer` shows build + kubectl logs → URL appears, opens in new tab
7. Check K8s resources: `kubectl get deploy,svc,ingress -n prod-mcpagent -l app.kubernetes.io/managed-by=mcpagent-prototype`
8. **Memory isolation:** Save a memory in project A → verify it appears in `Projects/A/memories/`, not in `Plans/memories/` or `Projects/B/memories/`
9. Multi-user: user A's projects not visible to user B
