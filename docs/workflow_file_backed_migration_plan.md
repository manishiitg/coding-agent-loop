# Workflow File-Backed Migration Plan

## Summary

Move workflow persistence out of the database and make each workflow workspace self-describing with a top-level `workflow.json`.

After this migration:

- Workflow definition, schedules, and assignment live in files
- Workflow discovery comes from scanning workspaces, not querying workflow presets
- Workflow config inheritance for execution comes from `workflow.json`
- Chat sessions and event history may remain in the database, but must no longer depend on workflow preset rows

This is a large migration, not a small storage swap. Today, workflow mode is tightly coupled to `preset_queries`, `workflows`, and `preset_query_id` across backend APIs, frontend stores, workflow execution, scheduling, assignment, and session restoration.

## Goals

- Remove database storage for workflow definitions
- Remove database storage for workflow schedules and workflow employee assignment
- Make workflow workspaces portable and versionable in git
- Preserve the current workflow inheritance model:
  - workflow defaults
  - step config overrides
  - step override UI state
- Keep chat sessions and chat events in the database for now
- Avoid storing secret values in workspace files

## Non-Goals

- Migrating chat history or event streams out of the database
- Replacing step-level planning files that already live in the workspace
- Persisting live workflow runtime state like `workflow_status` in the new manifest
- Storing a pinned `selected_run_folder` as part of workflow definition

## Current State

### Database-backed workflow definition

Workflow-wide defaults currently live in `preset_queries` and related models:

- `selected_servers`
- `selected_tools`
- `selected_skills`
- `selected_secrets`
- `selected_global_secret_names`
- `llm_config`
- `use_code_execution_mode`
- `use_tool_search_mode`
- `browser_mode`
- selected workflow folder

Representative references:

- `agent_go/pkg/database/schema.sql`
- `agent_go/pkg/database/models.go`
- `agent_go/cmd/server/server.go`

### Database-backed workflow state and API contracts

Workflow runtime/status APIs still use `preset_query_id` and DB workflow rows:

- workflow create/status/update APIs
- assignment APIs
- workflow tab/session linkage

Representative references:

- `frontend/src/services/api.ts`
- `frontend/src/services/api-types.ts`
- `frontend/src/components/workflow/WorkflowChatTabs.tsx`

### Frontend assumes "workflow = preset"

Workflow selection, loading, inheritance, and overview screens currently depend on workflow presets:

- `frontend/src/stores/useGlobalPresetStore.ts`
- `frontend/src/components/WorkflowsOverviewPage.tsx`
- workflow node components that inherit defaults from `activePreset`

### Existing file-backed workflow data

These already live in the workspace and should remain file-backed:

- `planning/plan.json`
- `planning/step_config.json`
- `planning/step_override.json`
- `variables/variables.json`

## Target Architecture

Each workflow workspace gets a top-level `workflow.json` as the canonical workflow definition file.

Example shape:

```json
{
  "id": "wf_123",
  "label": "Customer onboarding",
  "capabilities": {
    "selected_servers": ["github", "gws"],
    "selected_tools": [],
    "selected_skills": ["account-research"],
    "selected_secrets": [],
    "selected_global_secret_names": [],
    "browser_mode": "none",
    "use_code_execution_mode": false,
    "use_tool_search_mode": false,
    "pre_discovered_tools": [],
    "llm_config": {
      "phase_llm": {
        "provider": "openai",
        "model_id": "gpt-4.1"
      },
      "llm_allocation_mode": "tiered",
      "tiered_config": {
        "tier_1": {
          "provider": "openai",
          "model_id": "gpt-4.1"
        },
        "tier_2": {
          "provider": "openai",
          "model_id": "gpt-4.1-mini"
        },
        "tier_3": {
          "provider": "openai",
          "model_id": "gpt-4.1-nano"
        }
      }
    }
  },
  "execution_defaults": {
    "always_use_same_run": false,
    "skip_execution_cleanup": false
  },
  "ownership": {
    "employee_id": null
  },
  "schedules": []
}
```

## Manifest Rules

### Durable workflow definition

The manifest should contain only durable workflow definition and workflow-level defaults.

This includes:

- workflow identity
- display label
- workflow-level MCP server list
- workflow-level tools and skills
- browser mode
- workflow-level LLM config
- code execution and tool search defaults
- execution toolbar defaults
- employee assignment
- workflow schedules

### No runtime state in the manifest

Do not store live runtime state such as:

- `workflow_status`
- `selected_options`
- currently selected step
- currently selected run folder
- active execution progress

Those are runtime/session concerns, not workflow definition.

### No redundant booleans

Do not create duplicate sources of truth:

- Browser enablement should come from `browser_mode`
- GWS enablement should come from presence of `gws` in `selected_servers`

Avoid reintroducing:

- `enable_browser_access`
- `enable_gws_access`

### Secret safety

Never store secret values in `workflow.json`.

Allowed:

- secret names
- secret ids or references
- selected global secret names

Not allowed:

- plaintext API keys
- OAuth tokens
- resolved credential payloads

## Field Mapping

### Capabilities

Store workflow-wide agent and tool configuration in `capabilities`:

- `selected_servers`
- `selected_tools`
- `selected_skills`
- `selected_secrets`
- `selected_global_secret_names`
- `browser_mode`
- `use_code_execution_mode`
- `use_tool_search_mode`
- `pre_discovered_tools`
- `llm_config`

### LLM configuration

Preserve the current `PresetLLMConfig` shape under `capabilities.llm_config`.

That includes:

- default/root model fields already supported today
- `phase_llm`
- tiered allocation mode
- `tiered_config`
- execution or learning model fields already used by workflow nodes

This minimizes frontend and backend reshaping because the system already understands this structure.

### Execution defaults

Store workflow toolbar defaults in `execution_defaults`:

- `always_use_same_run`
- `skip_execution_cleanup`

Important:

- The toggle belongs in the workflow file
- The exact `selected_run_folder` does not

`selected_run_folder` should remain UI/session state and continue to be chosen at runtime.

### Ownership and schedules

Store these in the manifest:

- `ownership.employee_id`
- `schedules`

This removes workflow assignment and scheduling from DB-backed workflow records.

## Backend Scope

### 1. Workflow discovery

Replace workflow listing via presets with workspace discovery via `workflow.json`.

Backend needs:

- a scanner for valid workflow workspaces
- manifest parsing and validation
- stable workflow identity from `workflow.json.id`
- workspace path resolution from manifest location

### 2. Workflow read/write APIs

Replace workflow preset CRUD behavior for workflow mode with manifest-backed APIs.

Required changes:

- create workflow should create workspace files and `workflow.json`
- update workflow should write `workflow.json`
- workflow load should read `workflow.json`
- duplicate workflow should copy workspace files and generate a new workflow id
- delete workflow should operate on the workspace, not a preset row

### 3. Workflow execution bootstrap

Workflow execution currently reloads workflow defaults from DB-backed preset data. That must switch to manifest-backed loading.

Execution bootstrap must read from:

- `workflow.json.capabilities`
- `planning/step_config.json`
- `planning/step_override.json`

Inheritance order should remain:

- workflow manifest defaults
- step config
- step override UI state

### 4. Scheduling

Workflow schedules should be loaded from and persisted to `workflow.json`.

Required changes:

- schedule create/update/delete endpoints must target a workflow id or workspace path
- scheduler runtime must discover schedules from workflow manifests
- schedule execution must resolve workflow by manifest identity, not preset id

### 5. Assignment

Workflow employee assignment must move from DB-backed preset metadata to `workflow.json.ownership.employee_id`.

### 6. Versioning and publish/revert

Workflow version snapshots must include `workflow.json`.

This closes the current gap where workflow-wide defaults are not fully versioned with the workspace files.

### 7. Session restoration and chat linkage

Chat sessions may stay in the DB, but workflow mode must stop depending on `preset_query_id` for restoration.

Session metadata should instead carry:

- `workflow_id`
- `workspace_path`

Any existing workflow-session linking that assumes workflow preset existence must be rewritten.

## Frontend Scope

### 1. Workflow list and selection

Replace workflow preset loading with manifest-backed workflow loading.

This affects:

- workflow picker
- quick switcher
- workflows overview page
- active workflow selection

### 2. Active workflow state shape

The frontend should stop treating workflow mode as "active preset".

Introduce a workflow-shaped object derived from `workflow.json`, then migrate consumers that currently rely on `activePreset`.

### 3. Workflow editor and inheritance consumers

Workflow nodes currently inherit defaults from preset-backed workflow data.

Representative consumers:

- Loop node
- Todo Task node
- Decision node
- Routing Step node

These consumers must read from the new workflow object, but the inheritance semantics should stay the same.

### 4. Toolbar defaults

Move these persistent defaults from localStorage-only workflow state into `workflow.json.execution_defaults`:

- reuse iteration
- clean outputs / skip cleanup

Session-only state should remain local/session-backed:

- selected run folder
- active run selection
- transient execution UI state

### 5. Workflow assignment and schedules UI

Assignment and schedule dialogs should stop calling preset-based APIs and instead use workflow-id or workspace-path APIs backed by `workflow.json`.

## API Contract Changes

Workflow-mode endpoints should stop using `preset_query_id` as the primary key.

Preferred identifiers:

- `workflow_id`
- `workspace_path`

Likely contract updates:

- workflow load endpoints
- workflow update endpoints
- workflow create/duplicate/delete endpoints
- workflow schedule endpoints
- workflow assignment endpoints
- workflow status/update endpoints if they remain
- workflow chat/session restoration payloads

Generated event and API types will also need regeneration after the contract changes.

## Migration Strategy

Use a one-time cutover, not an indefinite dual-read system.

### Phase 1. Introduce manifest support

- Add manifest struct, parser, validator, and writer
- Add workflow discovery by scanning workspaces
- Add manifest-backed workflow APIs behind workflow mode

### Phase 2. Migrate existing workflows

Create a migration command that:

- finds existing workflow-mode presets
- resolves their workspace folder
- writes a `workflow.json` for each
- copies workflow-level config from preset rows into `capabilities`
- copies assignment into `ownership.employee_id`
- copies schedules into `schedules`
- maps toolbar defaults if currently stored elsewhere

Rules:

- fail on missing workspace folders
- fail on conflicting existing manifest unless explicit overwrite is requested
- never write secret values into the manifest

### Phase 3. Cut runtime over to manifest-only for workflow mode

- stop reading workflow presets for workflow mode
- stop reading workflow rows for workflow mode
- switch execution bootstrap to manifest-backed loading
- switch schedule execution to manifest-backed workflow resolution

### Phase 4. Remove preset-based workflow assumptions from frontend

- replace workflow preset stores/selectors with workflow manifest stores/selectors
- update workflow node inheritance consumers
- update workflow tabs and restoration metadata

### Phase 5. Cleanup

- remove dead workflow-mode preset code paths
- remove obsolete workflow DB access patterns
- keep non-workflow preset support unchanged

## Major Risks

### 1. Workflow identity drift

If workflow identity is derived from folder path instead of stored id, renames become dangerous.

Recommendation:

- store a stable `workflow.json.id`
- treat workspace path as location, not identity

### 2. Hidden `preset_query_id` coupling

There are many existing assumptions that workflow mode is preset-backed.

Risk areas:

- chat restoration
- workflow tabs
- schedule execution
- overview/list pages
- workflow APIs

### 3. LLM config compatibility

If `llm_config` is reshaped too aggressively, workflow nodes and execution code will break.

Recommendation:

- preserve the existing `PresetLLMConfig` shape inside the manifest

### 4. Secret leakage

Moving config to files increases the risk of accidentally writing resolved secrets into the workspace.

Recommendation:

- only persist references/names
- validate writes
- add tests that assert secret values are never serialized

### 5. Partial cutover bugs

If workflow mode reads some values from DB and some from files during rollout, mismatches will be hard to debug.

Recommendation:

- keep the dual-read period as short as possible
- move to manifest-only workflow reads once migration succeeds

## Testing Plan

### Backend tests

- manifest parse/validate/defaulting
- workflow discovery by scanning workspaces
- migration command produces correct `workflow.json`
- execution bootstrap reads workflow defaults from manifest
- schedule load/save uses manifest-backed schedules
- assignment load/save uses manifest-backed ownership
- version publish/revert includes `workflow.json`
- workflow duplicate creates a new workflow id

### Frontend tests

- workflow picker loads manifest-backed workflows
- workflow selection restores correct workspace
- workflow nodes inherit defaults from manifest-backed workflow state
- toolbar defaults persist through manifest save/load
- selected run folder remains session-local and is not persisted to the manifest
- assignment and schedules UI operate against new APIs

### Regression tests

- existing step config override behavior still works
- workflow execution uses same inheritance precedence as before
- chat history still loads for workflow sessions when linked by workflow id/workspace path
- non-workflow presets still work unchanged

## Recommended Execution Order

1. Add `workflow.json` schema, validation, and backend read/write support
2. Add workspace discovery for workflows
3. Add manifest-backed workflow APIs
4. Migrate existing workflow records into workspace manifests
5. Switch backend execution, schedule, and assignment logic to manifest-backed workflow resolution
6. Switch frontend workflow lists, selection, and editing to manifest-backed data
7. Switch workflow session linkage from `preset_query_id` to `workflow_id` and `workspace_path`
8. Remove dead workflow-mode preset paths

## Final Assessment

If the goal is truly "no DB at all for workflows," this should be treated as an architectural migration.

The hard part is not writing `workflow.json`.

The hard part is replacing the current assumption that:

`workflow = preset row + workflow row + preset_query_id linkage`

with:

`workflow = workspace + workflow.json + file-backed schedules/assignment`

That migration is very doable, but it needs a deliberate cutover plan and should not be estimated as a small config refactor.
