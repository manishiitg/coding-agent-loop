# Prompt System Fixes — 2026-03-24

## Summary
Comprehensive review and fix of the workflow interactive builder prompts. Focused on removing duplication, restructuring for coherence, fixing mode detection, and cleaning up bloat.

---

## 1. Restructured Workshop System Prompt

**File**: `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go`
**Template**: `interactiveWorkshopSystemTemplate` (line ~802)

### Before (scattered structure)
```
ROLE → CURRENT MODE → PLAN STEPS → PLAN DESIGN → WORKSHOP TOOLS → FILE LAYOUT →
WORKSPACE CONTEXT → EVALUATION → STEP EXECUTION WORKFLOW → STEP TYPES → RULES
```

### After (cohesive structure)
```
CURRENT MODE → CURRENT STATE → PLAN DESIGN → RUNNING STEPS → DEBUGGING →
OPTIMIZATION (optimizer only) → EVALUATION (eval only) → TOOLS REFERENCE →
FILE LAYOUT → CONSTRAINTS
```

### Changes
- **Removed ROLE section** — identity is in the intro paragraph; code-exec note moved to TOOLS REFERENCE; "never search source code" moved to CONSTRAINTS
- **Merged CURRENT STATE** — combined old WORKSPACE CONTEXT + CURRENT PLAN STEPS into one section with workspace path, run folder, step configs, progress, plan steps, and plan JSON
- **Merged PLAN DESIGN + STEP TYPES** — both builder-only sections combined into one coherent planning reference
- **Created RUNNING STEPS** — consolidated iterations, groups, execution procedure, auto-notifications, stopping tasks
- **Created DEBUGGING** — extracted from STEP EXECUTION WORKFLOW into standalone section with investigation workflow and root cause mapping
- **Moved TOOLS REFERENCE to end** — tool catalog is lookup material, not procedural guidance
- **Added FILE LAYOUT table** — compact table format with absolute path prefix (`/app/workspace-docs/{{.WorkspacePath}}/`)
- **Slimmed CONSTRAINTS** — only 4 cross-cutting rules that don't fit elsewhere

---

## 2. Mode-Gated Content (Reduced Bloat)

**File**: `interactive_workshop_manager.go`

| Content | Before | After |
|---------|--------|-------|
| OPTIMIZATION GUIDELINES (~200 lines) | builder + optimizer | optimizer only |
| EVALUATION section (~20 lines) | eval + builder | eval only |
| Human-Assisted Learning (~25 lines) | all modes | optimizer + debugger only |
| Tool Search instructions | all modes (even code-exec) | only when `UseToolSearchMode=true` |

---

## 3. Tool Search vs Code Execution Mode Detection

### Problem
Workshop agent in code-exec mode (gemini-cli) was getting Tool Search instructions instead of Code Execution instructions.

### Root Cause
`server.go` phase chat path used `req.Provider` (frontend value, e.g., `"vertex"`) instead of the resolved provider from DB preset (`"gemini-cli"`).

### Fix
**File**: `agent_go/cmd/server/server.go` (line ~4546)

```go
// Before: used req.Provider from frontend
phaseProvider := req.Provider

// After: uses finalProvider from DB preset (set at line 2106)
phaseIsCodeExec := finalProvider == "claude-code" || finalProvider == "gemini-cli" || finalProvider == "codex-cli"
phaseIsToolSearch := !phaseIsCodeExec
```

### Also fixed
- `UseToolSearchMode` was missing from template vars → added to both `interactive_workshop_manager.go:666` and `planning_exports.go:97` and `server.go:4564`
- Workshop agent `UseToolSearchMode` was hardcoded `true` even in code-exec mode → changed to `!config.UseCodeExecutionMode` (line 732)

---

## 4. Code Execution Instructions Missing from Workshop Agent

### Problem
Execution agents got full `{{TOOL_STRUCTURE}}` with API schemas from mcpagent, but the workshop agent only got a hand-written text note.

### Fix
**File**: `interactive_workshop_manager.go` (line ~1503)

Added injection of `prompt.GetCodeExecutionInstructions(workspacePath)` (which includes `{{TOOL_STRUCTURE}}` placeholder) or `prompt.GetToolSearchInstructions()` into the workshop agent's system prompt. The `{{TOOL_STRUCTURE}}` placeholder is resolved by `mcpagent.Agent.SetSystemPrompt()` with the actual tool index JSON.

**File**: `server.go` (line ~4670)

Phase chat path also injects code-exec/tool-search instructions from mcpagent, using `finalProvider` from DB for mode detection.

---

## 5. Duplicate Secrets in Prompt

### Problem
Secrets appeared twice: once from the workflow phase setup (`planning_exports.go` / `interactive_workshop_manager.go`) and once from the generic server handler (`server.go:5184`).

### Fix
**File**: `server.go` (line ~5180)

Added `isWorkflowPhase := req.PhaseID != ""` check — skips generic secret injection for workflow phases since they already inject secrets in the phase setup.

**File**: `interactive_workshop_manager.go` (line ~1544)

Removed workshop-level secret injection (was duplicating server.go's injection).

---

## 6. Duplicate Step Context in Optimization Agent

### Problem
The optimization agent prompt showed step details twice: individual fields (Step ID, Title, Description, etc.) AND the full plan.json entry as JSON.

### Fix
**File**: `interactive_workshop_manager.go` (line ~5654)

Kept only Step ID, Workspace, Run Folder as fields. The full JSON (which contains all the same info plus validation_schema) is the single source.

---

## 7. Browser Instructions — Merged Two Headings

### Problem
CDP mode appended two separate `##` sections: "Browser Automation (Quick Start)" + "Browser Mode: CDP". Redundant and confusing.

### Fix
**File**: `agent_go/pkg/instructions/browser.go`

Created merged functions:
- `GetCdpBrowserInstructions()` — single `## Browser Automation (CDP — Connected to User's Chrome)` section
- `GetHeadlessBrowserInstructions()` — single `## Browser Automation (Headless Container Browser)` section

Added CDP port auto-configuration clarity: agent does NOT need to pass any CDP URL/port — backend injects `--cdp` flag automatically.

---

## 8. Auto-Notification Consolidation

### Problem
Auto-notification mentioned in 6 places across the prompt (ROLE, Running Steps, Auto-Notification System, Debugging, RULES).

### Fix
Consolidated into single "Auto-Notification System" subsection under RUNNING STEPS. Removed scattered mentions from ROLE, RULES, and other sections.

---

## 9. Removed Emojis from Section Headers

**Files**: `interactive_workshop_manager.go`, `secrets_integration.go`, `server.go`

Stripped all emojis (🤖, 🎯, 📋, 📐, 📖, 🔍, ⚙️, 📂, ⚠️, 📊, 🏗️, 📁, 🔐) from `##` section headers. LLMs navigate by `##` structure, not emojis — they just waste tokens.

---

## 10. Frontend — Removed Auto "Files in Context"

### Problem
Every message in workflow mode auto-appended `📁 Files in context: Workflow/trading` even though the user didn't add any files.

### Fix
**File**: `frontend/src/components/ChatArea.tsx` (line ~2032)

Removed auto-population of `effectiveFileContext` from workflow preset's `selectedFolder`. Only user-added files (multi-agent mode) populate file context now.

---

## 11. Trading Workflow Plan Fixes

**File**: `workspace-docs/Workflow/trading/planning/plan.json`

- Removed redundant "find run folder" boilerplate from all 4 validation route descriptions (technical-check, fundamentals-deepdive, market-context, news-check)
- Fixed broken path concatenation (`...xargs dirname)technical_check.json`)
- Removed hardcoded stock names from market-context route (was: "metals/mining for NATIONALUM, exchange/financials for BSE, AMC for HDFCAMC")
- Cleaned up step-execute-trades pre-check description

---

## 12. Orphan Step Description Updated

**File**: `interactive_workshop_manager.go`

Updated Orphan step type description to emphasize its value as reusable utility agents for the builder (data checks, environment validation, one-off investigations).

---

## Files Modified

| File | Changes |
|------|---------|
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go` | Full template restructure, mode gating, code-exec injection, removed duplicates |
| `agent_go/cmd/server/server.go` | Mode detection from DB preset, duplicate secrets fix, emoji removal |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_exports.go` | Added `UseToolSearchMode` to template vars |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/secrets_integration.go` | Removed emoji from secrets header |
| `agent_go/pkg/instructions/browser.go` | Merged CDP/headless browser instructions, added CDP auto-config docs |
| `frontend/src/components/ChatArea.tsx` | Removed auto file context for workflow mode |
| `workspace-docs/Workflow/trading/planning/plan.json` | Fixed validation route descriptions |
