# Persistent Stores Design

This document captures the planned improvements to how workflows manage persistent data, report output, and knowledgebase access control. None of these are implemented yet — this is the design spec.

---

## Background: Current Stores

Workflows currently have three persistent stores at the workspace root:

| Store | Location | Written by | Purpose |
|-------|----------|------------|---------|
| **Learnings / Skills** | `learnings/_global/` | Learning agent (automatic) | HOW to execute — patterns, selectors, auth flows |
| **Knowledgebase** | `knowledgebase/` | Any step agent | Domain facts + doubles as persistent data store |
| **Reports** | `reports/{group}/` | Report agent | Human-readable run summary |

The tension: **knowledgebase is doing two jobs** — storing domain context to help agents make better decisions, and acting as a database for structured data that needs to survive across runs. These have different access patterns and formats and should be separated.

---

## 1. New `db/` Folder

### What it is

A dedicated folder for **structured persistent data in JSON format**, separate from the knowledgebase. Steps use it to store state, results, or any data that needs to survive across runs and be readable by later steps or other groups.

### Folder structure

```
workspace/
├── db/               ← new, always enabled, JSON files only
├── knowledgebase/    ← graph-structured domain knowledge (see section 4)
├── learnings/        ← procedural skill knowledge (HOW to run)
├── reports/          ← human-readable run summaries
└── runs/
```

### Behaviour

- Always created on workspace init (no toggle, unlike knowledgebase)
- All steps get read + write access by default
- JSON format by convention — no schema enforcement, agents are instructed to use `.json` files
- Never deleted during cleanup
- Injected into execution agent prompt as an absolute path

### Key implementation touchpoints

| File | Change |
|------|--------|
| `controller_execution.go` | Add `DBFolderName = "db"` constant + `getDBPath()` |
| `controller_run_manager.go` | Always create `db/` on init (no `UseKnowledgebase`-style gate) |
| `controller_agent_factory.go:397` | Add `db/` to read+write in `setupExecutionFolderGuard` |
| `controller_agent_factory.go:806` | Same for orchestrator agent guard |
| `controller_todo_task.go:91` | Add `db/` to read+write paths |
| `execution_only_agent.go` | Tell agents about `db/` path and JSON convention |
| `todo_task_orchestrator_agent.go` | Same |
| `pre_validation.go:1080` | Add `"db"` to known workflow folder names |
| `evaluation_types.go` | Add `DBWrite bool` to `EvaluationStep` |
| `controller_agent_factory.go:800` | Evaluation folder guard: always add `db/` to read; add to write if `DBWrite: true` |

### Evaluation runs and `db/`

Evaluation steps can also read and write `db/` but write access must be explicitly granted per evaluation step. This mirrors the `knowledgebase_access` pattern for regular steps.

Add a `db_write` boolean to `EvaluationStep` in `evaluation_types.go`:

```go
type EvaluationStep struct {
    ID              string
    Title           string
    Description     string
    PreValidation   *ValidationSchema
    SuccessCriteria string
    ContextOutput   string
    DBWrite         bool `json:"db_write,omitempty"` // If true, evaluation step can write to db/
}
```

Folder guard for evaluation agents (`controller_agent_factory.go:800`):
- **Read**: always add `db/` to read paths
- **Write**: add `db/` to write paths only if `DBWrite: true` on the evaluation step

### Open questions

- Should `db/` writes be blocked from learn-code scripts (similar to the `knowledgebase/learnings/` check in `controller_learn_code.go:457`)?

---

## 2. Dynamic Report System

### What changes from current

| | Current | Proposed |
|--|---------|----------|
| **Report source** | Raw execution artifacts from `runs/{runFolder}/execution/` | `db/` files + `knowledgebase/graph.json` (workspace-scoped) |
| **Generation** | Report agent runs once, produces static markdown | No agent — frontend renders live from `report_plan.md` |
| **Storage** | `reports/{group}/{timestamp}.md` archive | No archive — always reflects current `db/` state |
| **Definition** | `planning/output_plan.json` (instructions for the agent) | `planning/report_plan.md` (markdown widget definitions) |
| **If no data** | Empty report with headings | Report not shown at all |

### `report_plan.md` format

Stored at `planning/report_plan.md`. Created and maintained by the workflow interactive builder.

`##` headings define sections. Fenced blocks define widgets. A `widget:row` block groups widgets side-by-side. Standalone widget blocks are full-width.

````markdown
## Overview

```widget:text
source: db/summary.json
path: description
```

```widget:row
- chart | source: db/results.json | path: counts
- chart | source: db/monthly.json | path: totals
```

## Companies Found

```widget:table
source: knowledgebase/graph.json
path: entities
filter: type=company
```
````

### Widget types

Three types only:

| Block | Description | Data shape at `path` |
|-------|-------------|---------------------|
| `widget:text` | Markdown text block | String value |
| `widget:chart` | Bar or line chart | `[{ "label": string, "value": number }]` |
| `widget:table` | Data table — columns from object keys | Array of objects |
| `widget:row` | Layout wrapper — groups widgets side-by-side | — |

Inside `widget:row`, each line is `- {type} | source: {path} | path: {key}`.

### Widget fields

| Field | Required | Description |
|-------|----------|-------------|
| `source` | yes | `db/{file}.json` or `knowledgebase/graph.json` |
| `path` | yes | Dot-notation key into the JSON file |
| `filter` | no | `key=value` — filters array items (useful for KB entities) |

### Data sources

| Source | What it points to |
|--------|------------------|
| `db/{file}.json` | Any JSON file in the workspace-level `db/` folder |
| `knowledgebase/graph.json` | KB graph — entities and relationships |

Both are workspace-scoped. The report always reflects the latest state.

**If `db/` has no data yet**, the report is not shown — no empty state, no placeholder widgets.

### How the frontend renders it

1. Fetch and parse `planning/report_plan.md`
2. Split on `##` headings → sections
3. For each fenced block, parse type + fields
4. For `widget:row`, parse the inline widget list
5. For each widget, fetch `source` file, walk `path`, apply `filter`
6. If any required source file is missing or resolved data is empty → do not render the report
7. Render each widget using its type component

### Who creates `report_plan.md`

The **workflow interactive builder** (chat phase). The user describes what they want to see; the builder agent writes the markdown document based on what steps write to `db/` and what the KB captures. Since the builder knows the step plan, it can infer the right `source` paths.

### Report is not triggered by workflow execution

The report is no longer a phase that runs after execution completes. It is purely a frontend view — opened on demand. `db/` is populated during normal step execution and evaluation runs, so the report data is always there when the user opens it.

- `MaybeRunAutoFinalOutput()` is removed — no post-run auto-trigger
- The "Final Report" button in the toolbar no longer starts a phase — it just opens the report viewer
- The report viewer checks whether `db/` has data; if not, it shows nothing

### What goes away

- `final_output.go` — report agent removed entirely
- `planning/output_plan.json` — replaced by `planning/report_plan.md`
- `reports/` folder — no more archive files
- `WorkflowStatusReportExecution = "report-execution"` phase constant
- `runReportExecutionOnly()` in `workflow_orchestrator.go`
- `MaybeRunAutoFinalOutput()` — no post-run auto-trigger
- `handleGetFinalOutputs`, `handleGenerateFinalOutput`, `handleGetFinalOutputConfig`, `handleUpdateFinalOutputConfig` HTTP handlers
- `FinalOutputPopup.tsx` — replaced by dynamic report viewer

### Key implementation touchpoints

| File | Change |
|------|--------|
| New: `frontend/src/components/workflow/ReportViewer.tsx` | Parses `report_plan.md`, resolves sources, renders widgets |
| New: `frontend/src/components/workflow/widgets/TableWidget.tsx` | Table renderer |
| New: `frontend/src/components/workflow/widgets/TextWidget.tsx` | Markdown text renderer |
| New: `frontend/src/components/workflow/widgets/ChartWidget.tsx` | Bar/line chart (reuses existing SVG chart logic from `MarkdownRenderer.tsx`) |
| `frontend/src/services/api.ts` | Add `getReportPlan()`, `getDBFile()` API calls |
| `frontend/src/services/api-types.ts` | Add `ParsedReportPlan`, `ReportSection`, `ReportWidget` types |
| `WorkflowToolbar.tsx` | "Report" button opens `ReportViewer` (no phase start) |
| `cmd/server/workflow.go` | Add `handleGetReportPlan()`, `handleGetDBFile()` handlers; remove old final-output handlers |
| `workflowtypes/types.go` | Remove `WorkflowStatusReportExecution` |
| `workflow_orchestrator.go` | Remove `runReportExecutionOnly()` routing |
| Delete: `final_output.go` | Report agent no longer needed |

---

## 3. Step-Level Knowledgebase Access Control

### What it is

Replace the binary `disable_knowledgebase` per-step flag with an explicit access mode: `"read"`, `"write"`, `"read-write"`, or `"none"`. The folder guard enforces the mode.

### Current state

- `UseKnowledgebase bool` at preset/workflow level
- `DisableKnowledgebase *bool` in `AgentConfigs` — only disables, can't make a step read-only

### Proposed: `knowledgebase_access` + `knowledgebase_contribution` fields

```json
// planning/step_config.json
{
  "steps": [
    {
      "step_id": "step-1",
      "agent_configs": {
        "knowledgebase_access": "read"
      }
    },
    {
      "step_id": "step-2",
      "agent_configs": {
        "knowledgebase_access": "read-write",
        "knowledgebase_contribution": "Extract company names, industries, and decision-maker contacts from the scraped profiles. Focus on companies with 50-200 employees in the SaaS space."
      }
    }
  ]
}
```

`knowledgebase_contribution` is a plain string instruction that tells the KB update agent **what to extract** from this step's output and how to represent it in the graph. It is:
- Only relevant when `knowledgebase_access` is `"write"` or `"read-write"`
- Set by the user in the workflow interactive builder (not auto-generated)
- The trigger for the KB update agent — if not set, the KB update agent does not run for that step even if write access is granted

Valid values:

| Value | Read | Write | Notes |
|-------|------|-------|-------|
| `"read-write"` | yes | yes | Default when knowledgebase is enabled |
| `"read"` | yes | no | Step can consume but not modify |
| `"write"` | no | yes | Step can append/update but not read |
| `"none"` | no | no | Equivalent to `disable_knowledgebase: true` |

`DisableKnowledgebase *bool` is kept for backward compatibility and maps to `"none"` / `"read-write"`.

### Resolution logic

```
resolveKnowledgebaseAccess(stepConfig, presetEnabled):
  1. knowledgebase_access field set → use it directly
  2. disable_knowledgebase: true    → "none"
  3. disable_knowledgebase: false   → "read-write"
  4. preset enabled                 → "read-write"
  5. preset disabled                → "none"
```

### Key implementation touchpoints

| File | Change |
|------|--------|
| `planning_agent.go:227` | Add `KnowledgebaseAccess string` and `KnowledgebaseContribution string` to `AgentConfigs`; keep `DisableKnowledgebase` as deprecated |
| `controller_execution.go:1065` | Replace bool resolution with `resolveKnowledgebaseAccess()` helper |
| `controller_agent_factory.go:397` | `setupExecutionFolderGuard` takes `kbAccess string` instead of `...bool`; adds to read/write paths separately |
| `controller_agent_factory.go:806` | Same for orchestrator agent guard |
| `controller_learn_code.go:642` | Use same `resolveKnowledgebaseAccess()` helper |
| `execution_only_agent.go` | Prompt reflects actual access mode (e.g. `READ` vs `READ/WRITE`) |
| `todo_task_orchestrator_agent.go` | Same |

---

## 4. Knowledgebase Graph Structure

### Motivation

Currently the knowledgebase has no enforced shape — agents write whatever they want. This makes it hard to query, merge across runs, or reason about what's known. A **file-based graph structure** gives it a fixed, queryable shape without requiring a graph database.

Inspired by graph-based agent memory research (Graphiti, GraphRAG, AGENTiGraph): knowledge is modelled as **entities (nodes)** and **relationships (edges)**. The basic unit is a triple: `(subject, predicate, object)`. Each fact has provenance (which step/run created it) and a validity window (when it was created, whether it was superseded).

### File layout

```
knowledgebase/
  graph.json    ← source of truth: entities + relationships
  index.json    ← lightweight overview: counts, types, last updated
  raw/          ← unstructured source material if needed
```

Execution agents get the `knowledgebase/` path injected (not the content — same as today). They read what they need via shell:

```bash
cat knowledgebase/index.json                                           # quick overview
cat knowledgebase/graph.json | jq '.entities[] | select(.type=="company")'
```

`graph.json` serves both consumers: the KB update agent (merge logic) and execution agents (on-demand reads). No separate summary file needed.

### `graph.json` schema

```json
{
  "version": "1",
  "updated_at": "2026-04-15T10:00:00Z",
  "entities": [
    {
      "id": "company-acme",
      "type": "company",
      "label": "Acme Corp",
      "properties": {
        "website": "https://acme.com",
        "industry": "SaaS"
      },
      "created_at": "2026-04-15T09:00:00Z",
      "source": { "step": "step-1", "run": "iteration-2/group-a" }
    }
  ],
  "relationships": [
    {
      "id": "rel-001",
      "from": "company-acme",
      "to": "person-john",
      "type": "has_contact",
      "properties": {
        "role": "CEO"
      },
      "created_at": "2026-04-15T09:00:00Z",
      "source": { "step": "step-2", "run": "iteration-2/group-a" }
    }
  ]
}
```

### `index.json` schema

A lightweight summary so agents can decide whether to load the full graph:

```json
{
  "entity_count": 42,
  "relationship_count": 87,
  "entity_types": ["company", "person", "product", "event"],
  "relationship_types": ["has_contact", "owns", "competes_with"],
  "last_updated": "2026-04-15T10:00:00Z",
  "last_updated_by": { "step": "step-3", "run": "iteration-2/group-a" }
}
```

### Key design principles

- **Agents write via the KB update agent** (see section 5) — step execution agents do not write directly to `graph.json`
- **Agents read freely** — execution agents with `knowledgebase_access: read` or `read-write` can shell-read `graph.json` or `index.json` directly
- **No graph DB required** — plain JSON files, readable with any shell tool (`cat`, `jq`)
- **Source provenance on every fact** — every entity and relationship records which step/run wrote it, enabling auditability and conflict detection
- **Purely JSON** — no markdown files in the knowledgebase; `graph.json` is the source of truth for both machine merging and LLM reading

---

## 5. KB Update Agent (Post-Step)

### What it is

A dedicated agent that runs **after each step completes** (parallel to the success learning agent) and updates `knowledgebase/graph.json` with facts extracted from that step's output. Mirrors the learning agent pattern exactly.

### When it runs

```
Step execution
    ↓
Validation passes
    ↓
[goroutine A] Success learning agent  ← existing, updates learnings/_global/
[goroutine B] KB update agent         ← new, updates knowledgebase/graph.json
```

Both run in background, non-blocking. The KB update agent only runs if **all three** conditions are met:
- Knowledgebase is enabled at the preset level
- `knowledgebase_access` for the step is `"write"` or `"read-write"`
- `knowledgebase_contribution` is set for the step (non-empty)

If `knowledgebase_contribution` is not set, the agent is skipped entirely — there is no generic fallback extraction.

### What it reads

- Step execution output (`runs/{runFolder}/execution/{step-id}/`)
- `knowledgebase_contribution` from step config — the user-defined extraction instruction
- Current `knowledgebase/graph.json` (to merge, not overwrite)
- `knowledgebase/index.json` (for context on what's already known)

### What it writes

- Updated `knowledgebase/graph.json` — merges new entities and relationships
- Updated `knowledgebase/index.json` — refreshed counts, types, last_updated

### Agent instructions

The `knowledgebase_contribution` string is the primary directive. Fixed rules on top:
- Extract **WHAT** the workflow discovered — entities, facts, relationships found in the output
- Do NOT capture HOW the step ran (that's for learnings)
- Merge carefully — do not duplicate existing entities, update properties if newer
- Record `source` on every new entity and relationship

### Key implementation touchpoints

| File | Change |
|------|--------|
| `controller_execution.go:2055` | Trigger KB update agent goroutine after validation passes (alongside learning trigger) |
| New file: `controller_kb_update.go` | KB update orchestration logic (mirrors `controller_learning.go`) |
| New file: `kb_update_agent.go` | Agent definition and system prompt (mirrors `learning_agent.go`) |
| `controller_agent_factory.go` | Factory method for KB update agent |

### Folder guard for KB update agent

- **Read**: `runs/{runFolder}/execution/{step-id}/` + `knowledgebase/`
- **Write**: `knowledgebase/` only

---

## 6. Global KB Update Tool (Full-Run)

### What it is

A **manual phase** (`kb-update`) that processes all step outputs from a completed run and rebuilds or enriches the knowledgebase from scratch. Analogous to the report generation phase — triggered from the toolbar, not automatic.

Use cases:
- Knowledgebase was disabled during a run, now you want to populate it retrospectively
- A run completed before the KB update agent existed
- `knowledgebase_contribution` was updated after the run and you want to re-extract

Per-step `knowledgebase_contribution` from `step_config.json` drives the extraction — steps without a contribution defined are skipped, same as the automatic post-step flow.

### Flow

```
User clicks "Update KB" in toolbar → selects run folder
    ↓
POST /api/workflow/knowledgebase/update
    ↓
KBUpdateOrchestrator reads all outputs from runs/{runFolder}/execution/
    ↓
KB update agent processes each step's output sequentially
    ↓
graph.json + index.json written/merged
    ↓
Frontend: KBPopup refreshes
```

### HTTP endpoint

```
POST /api/workflow/knowledgebase/update
Body: { workspace_path, run_folder, merge_strategy: "merge" | "replace" }
```

- `"merge"` — adds new entities/relationships, updates existing ones if newer
- `"replace"` — clears knowledgebase and rebuilds from this run's outputs

### Key implementation touchpoints

| File | Change |
|------|--------|
| `workflowtypes/types.go` | Add `WorkflowStatusKBUpdate = "kb-update"` |
| `workflow_orchestrator.go` | Route `kb-update` status to `runKBUpdateOnly()` |
| New file: `kb_update_execution.go` | Full-run KB update logic (mirrors `evaluation_execution.go`) |
| `cmd/server/workflow.go` | Add `handleKBUpdate()` handler + register route |
| `cmd/server/server.go` | Register `POST /api/workflow/knowledgebase/update` |

---

## 7. Interactive Builder Changes

The workflow interactive builder is a chat-based phase (`workflow-builder`) that lets users modify the workflow plan via natural language. The following UI additions are needed to support the features above.

### New: KB Management Popup

A new popup component (`KBPopup.tsx`) parallel to `LearningsPopup.tsx` and `FinalOutputPopup.tsx`:

- **View**: renders `knowledgebase/index.json` summary (entity count, types, relationship types, last updated)
- **Explore**: expandable entity list from `graph.json`
- **Run KB Update**: triggers the full-run KB update phase (section 6) for the selected run folder
- **Clear**: deletes and recreates `knowledgebase/graph.json` and `index.json`
- **Export**: downloads `graph.json`

### New: KB Update toolbar button

In `WorkflowToolbar.tsx` alongside the existing "Final Report" button:

```
"Update KB" → opens KBPopup → user selects run folder → clicks "Run KB Update"
    ↓
handleRunKBUpdate() → onStartPhase('kb-update', { selected_run_folder })
```

### Updated: Step config UI — KB access mode + contribution

In the per-step config panel, replace the existing `disable_knowledgebase` toggle with:

```
Knowledgebase access: [ read-write ▼ ]
                       read
                       write
                       read-write
                       none

KB contribution:
┌─────────────────────────────────────────────────────────┐
│ Extract company names, industries, and decision-maker   │
│ contacts from the scraped profiles. Focus on companies  │
│ with 50-200 employees in the SaaS space.                │
└─────────────────────────────────────────────────────────┘
```

- The contribution textarea is only shown when access is `"write"` or `"read-write"`
- Saving writes both `knowledgebase_access` and `knowledgebase_contribution` to `step_config.json`
- The interactive builder (workflow-builder chat phase) can also set these fields via plan modification tools — `knowledgebase_contribution` should be one of the fields the builder agent can populate when it adds or configures a step

### Updated: Preset modal

The existing `use_knowledgebase` toggle in `PresetModal.tsx` stays as the workflow-level gate. No change needed here.

### Key implementation touchpoints

| File | Change |
|------|--------|
| New: `frontend/src/components/workflow/KBPopup.tsx` | KB management popup |
| `WorkflowToolbar.tsx` | Add "Update KB" button + `handleRunKBUpdate()` |
| Step config panel component | Replace `disable_knowledgebase` toggle with `knowledgebase_access` dropdown |
| `frontend/src/services/api.ts` | Add `updateKnowledgebase()`, `getKnowledgebaseIndex()`, `clearKnowledgebase()` API calls |
| `frontend/src/services/api-types.ts` | Add `KBIndex`, `KBUpdateRequest`, `KBUpdateResponse` types |

---

## 8. `soul.md` — Builder Persistent Memory

### What it is

A markdown file that the **workflow interactive builder reads on every session start and writes to over time**. It accumulates context that falls out of conversations with the user — things that have nowhere else to go in the structured files.

No other agent reads or writes it. It is purely the builder's long-term memory for this workflow.

### What it is NOT

- Not a duplicate of `plan.json` — `objective` and `success_criteria` stay in `plan.json` as one-liners for the runtime and learning agent to consume
- Not workflow config — capabilities, LLM settings, schedules stay in `workflow.json`
- Not execution knowledge — HOW to run steps stays in `learnings/`
- Not domain facts — discovered entities stay in `knowledgebase/`

### What it contains

Two things:

**1. User-specific context that has nowhere else to go**
- Why the objective is what it is — the story behind the one-liner in `plan.json`
- Decisions made during conversations ("tried bulk approach, too slow, switched to group-by-industry")
- Constraints the user has stated ("never process same company twice", "output must match CRM format")
- Things the builder has learned about the user's preferences over time

**2. Curated references to important files across other stores**
- Which `db/` files matter and what they contain
- Which KB entity types are relevant
- Where the key skill content lives in `learnings/`

This makes `soul.md` a **navigation layer** — a builder can start a fresh session, read `soul.md`, and immediately know where the important things live across all stores.

### Relationship to `objective` and `success_criteria`

`plan.json` keeps the one-liner versions of these because:
- The execution orchestrator sets `hcpo.SetObjective()` from `plan.json` at runtime
- The learning agent receives `CurrentObjective` from `hcpo.GetObjective()`
- Parsing structured JSON is reliable; parsing free-form markdown is not

`soul.md` carries the **richer version** — the context, evolution, and nuance behind them:

```
plan.json:   "objective": "Scrape and qualify LinkedIn companies"

soul.md:     ## Why
             Originally this was a bulk scrape. After two failed runs the user
             decided to switch to group-by-industry to reduce rate limiting.
             The real goal is to build a qualified list for outreach — quality
             over quantity. Aim for 20 strong leads per run, not 200 weak ones.
```

They don't need to be identical. `plan.json` has the headline; `soul.md` has the story.

### Format

```markdown
---
workflow: LinkedIn Outreach
updated: 2026-04-15
---

## Why
[Context behind the objective — things the user explained in chat
that aren't captured in the one-line objective in plan.json]

## Decisions & Constraints
- Only target SaaS companies 50–200 employees
- Skip companies already in db/processed.json
- Output format must match CRM import schema (columns: name, email, title, company)
- Tried bulk scrape approach — too slow and rate-limited, switched to group-by-industry

## Key References
- Processed companies: db/processed.json
- Target company graph: knowledgebase/graph.json (entity type: company)
- Execution skill: learnings/_global/SKILL.md
- Current run results: db/results.json

## Notes
[Free-form discoveries, things to revisit, open questions]
```

### When the builder writes to it

- **Automatically** when the user makes a decision worth preserving ("we'll skip companies under 10 employees")
- **Automatically** when a new `db/` file or KB entity type becomes important
- **On user request** — "remember this" or "save this decision"
- **At session start** if `soul.md` doesn't exist yet — builder creates it from what it can infer from `plan.json` and existing stores

The builder does not ask for confirmation on every write — it writes silently, like the memory system. If the user wants to review, they can open `soul.md` directly.

### Location

```
planning/soul.md
```

Same directory as `plan.json`, `step_config.json`, `report_plan.md`. The builder owns this directory.

### Key implementation touchpoints

| File | Change |
|------|--------|
| `interactive_workshop_manager.go` | Load `planning/soul.md` at session start; inject as `SoulContent` template var into all workshop mode prompts |
| `interactive_workshop_manager.go` | After significant decisions, write/update `soul.md` via shell tool |
| `cmd/server/workflow.go` | Add `handleGetSoul()`, `handleUpdateSoul()` HTTP handlers |
| `WorkflowToolbar.tsx` | Expose `soul.md` in the workspace file browser (already visible if file exists) |

---

## References

- [Graphiti — Real-Time Knowledge Graphs for AI Agents](https://github.com/getzep/graphiti)
- [Graph-Based Agent Memory: Taxonomy, Techniques, and Applications](https://arxiv.org/html/2602.05665v1)
- [GraphRAG and Agentic Architecture (Neo4j)](https://neo4j.com/blog/developer/graphrag-and-agentic-architecture-with-neoconverse/)
- [Why Knowledge Graphs for LLM Personalization (Memgraph)](https://memgraph.com/blog/why-knowledge-graphs-for-llm)
