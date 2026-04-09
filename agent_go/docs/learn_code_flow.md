# Learn Code (Scripted Code) Execution Flow

## Overview

Learn code mode makes step execution **reproducible**. The LLM writes a `main.py` script that gets saved to `learnings/{step-id}/`. On subsequent runs, the saved script runs first (0 LLM tokens). If it fails, the LLM fixes it.

## Modes

There are two code execution modes, set via `AgentConfigs.DeclaredExecutionMode`:

| Mode | Config | Script Persisted | Fast Path | When to Use |
|------|--------|-----------------|-----------|-------------|
| `learn_code` | `DeclaredExecutionMode: "learn_code"` | Yes → `learnings/{step-id}/` | Yes (0 tokens) | Steps that should be reusable across runs |
| `code_exec` | `UseCodeExecutionMode: true` | No (ephemeral) | No (always calls LLM) | One-off steps, exploration |

The mode is set per-step via the interactive workshop manager or `step_configs.json`. There is **no automatic promotion** from `code_exec` to `learn_code` — it's always config-driven.

`learn_code` implies `code_exec` (forces `isCodeExecutionMode = true`).

## Key Paths

| Path | Purpose | Persistence |
|------|---------|-------------|
| `learnings/{step-id}/main.py` | Saved script (best known version) | Persistent across runs |
| `learnings/{step-id}/diffs/` | Version diffs between saved scripts | Persistent |
| `execution/{step-path}/code/main.py` | Working copy (LLM writes/fixes here) | Preserved within a run, cleaned between runs |
| `execution/{step-path}/code/fix-diffs/` | Diffs between fix iterations within a run | Per-run |
| `execution/{step-path}/` | Step output files (STEP_OUTPUT_DIR) | Cleaned before each script run (code/ preserved) |
| `learnings/_global/SKILL.md` | Global skill file | Persistent |

## Execution Flow

### Phase 1: Static Code Review

Before running the saved script, `reviewMainPyScript()` performs static checks:

1. **Hardcoded execution paths** — `/app/workspace-docs/.../runs/iteration-N/.../execution` patterns
2. **`os.environ.get()` with hardcoded fallback** for declared `VAR_*`/`SECRET_*` variables
3. **`os.environ.get()` with fallback** for system env vars (`STEP_OUTPUT_DIR`, `MCP_API_URL`, etc.)
4. **Sibling step folder references** without `STEP_EXECUTION_DIR`
5. **Long hardcoded workspace paths** (`/app/workspace-docs/...` 20+ chars)
6. **File writes without `STEP_OUTPUT_DIR`** — `open('file', 'w')` not using env var
7. **Manual sibling paths when `sys.argv` available** — constructs paths instead of using argv
8. **Writes to system-managed directories** — scripts writing to `knowledgebase/` or `learnings/` (cache shortcuts)
9. **Hardcoded cache/shortcut variables** — `CACHE_DIR = '/app/...'` patterns that bypass actual work

If any issues are found → **fast path is skipped**, LLM is called with the issues listed.

### Phase 2: Saved Script (Fast Path)

```
tryRunSavedLearnCodeScript()
  1. Check: does learnings/{step-id}/main.py exist?
     - No  -> skip to Phase 3 (LLM generates from scratch)
     - Yes -> continue

  2. Static code review (Phase 1)
     - Issues found -> skip to Phase 3 (LLM fixes issues)

  3. Check: does execution/{step-path}/code/main.py already exist?
     - Yes -> use it (may be LLM-fixed from previous attempt)
     - No  -> copy all files from learnings/{step-id}/ to execution/{step-path}/code/

  4. Clean output files in execution/{step-path}/ (preserve code/)

  5. Run main.py from execution/{step-path}/code/
     - STEP_OUTPUT_DIR = execution/{step-path}/
     - SCRIPT_VERBOSE=1 (always set for debug output)
     - All SECRET_*, VAR_*, MCP_API_URL, MCP_API_TOKEN injected as env vars

  6. Run pre-validation on outputs
     - Script exit 0 + validation passes -> SUCCESS (skip LLM entirely)
     - Script fails or validation fails -> Phase 3
```

### Phase 3: LLM Execution

```
executionAgent.Execute()
  - System prompt: code execution instructions, Python best practices, env var mappings
  - User message: task description, orchestrator instructions, prior script + error (if relearn)
  - LLM writes main.py to execution/{step-path}/code/main.py
  - LLM runs it, calls MCP tools via API, debugs as needed
  - LLM's turn ends
```

### Phase 4: Validation Fix Loop (up to 3 fix iterations)

```
After LLM's turn:
  for fixIter 0..maxFixIter (default 3):

    1. Run pre-validation on outputs
       - Passes + main.py exists -> SUCCESS
       - fixIter == maxFixIter -> EXHAUSTED, go to Phase 5

    2. Build feedback message (self-contained):
       - Task description
       - Current main.py content (read from disk)
       - Static code review issues (reviewMainPyScript)
       - Last execution output + exit code
       - Validation errors (which files missing, which checks failed)
       - Fix attempt counter (N/max)

    3. Create NEW repair agent (Tier 1/High) — fresh agent each iteration
       - Fresh conversation (no history from prior attempts)
       - Feedback message contains all context needed
       - Avoids accumulated confusion from prior failed attempts

    4. LLM fixes main.py, re-runs it

    5. Capture diff between fix iterations
       - Saved to execution/{step-path}/code/fix-diffs/fix-{N}-to-{N+1}.diff

    6. Repeat (back to step 1)
```

### Phase 5: Fallback to Code Exec (outer retry)

```
If learn_code fix loop exhausted:
  - isLearnCodeMode = false (disabled for remaining retries)
  - Save main.py to learnings (unless syntax errors) — latest attempt is 
    likely better than the known-broken version
  - LLM uses tools directly (no main.py requirement)
  - Pre-validation still checks outputs
  - Up to 2 more attempts (3 outer retries total)
```

## Complete Retry Structure

```
Retry Attempt 1 (learn_code mode):
  ├─ Fast path: run saved learnings/main.py (0 tokens)
  │   ├─ Success → DONE
  │   └─ Fail → LLM execution
  ├─ LLM writes/runs main.py (Phase 3)
  ├─ Fix iter 0: pre-validation fails → NEW repair agent (Tier 1)
  ├─ Fix iter 1: pre-validation fails → NEW repair agent (Tier 1)
  ├─ Fix iter 2: pre-validation fails → NEW repair agent (Tier 1)
  ├─ Fix iter 3: pre-validation fails → EXHAUSTED
  ├─ Save main.py to learnings (unless syntax errors)
  └─ Switch to code_exec mode

Retry Attempt 2 (code_exec mode — no main.py required):
  ├─ Fresh execution agent, LLM calls MCP tools directly
  └─ Pre-validation check → pass/fail

Retry Attempt 3 (code_exec mode):
  ├─ Fresh execution agent
  └─ Pre-validation check → pass/fail → if still failing, step fails
```

## Script Persistence

### When is execution/code/ → learnings/ saved?

**Always after the fix loop**, regardless of success or failure (unless syntax errors). The rationale: the learnings script already failed, so any LLM-produced version is a newer attempt and likely better.

- Fix loop succeeds → save to learnings (working script)
- Fix loop fails → save to learnings (latest attempt, unless SyntaxError)
- Fast path succeeds → save to learnings (may be LLM-fixed from previous run)

### When is execution/code/ cleaned?

- **Output files** (execution/{step-path}/): cleaned before each script run
- **Code directory** (execution/{step-path}/code/): **never cleaned** — preserved across retries so LLM fixes survive
- Code directory is only cleaned when the outer workflow starts a fresh iteration

### Diff Tracking

Two levels of diff tracking:

1. **Version diffs** (`learnings/{step-id}/diffs/main.py.v{N}.diff`): what changed between saved versions across runs
2. **Fix iteration diffs** (`execution/{step-path}/code/fix-diffs/fix-{N}-to-{N+1}.diff`): what changed between fix attempts within a single run

## Success Learning

After a step passes pre-validation, success learning may run:

```
Execution succeeds → Pre-validation passes → validationResponse.IsSuccessCriteriaMet = true
  │
  ├─ Learning disabled?           → Skip
  ├─ Learnings locked?            → Skip
  ├─ tempLLM override used?       → Skip (but update metadata for auto-lock threshold)
  │
  └─ Otherwise → runSuccessLearningPhase (runs in BACKGROUND)
       ├─ Analyzes conversation history
       ├─ Updates plan.json learnings
       └─ Updates learning metadata (success count, turn count, etc.)
```

When learning is skipped due to tempLLM, metadata is still updated so the success count increments toward the auto-lock threshold (3 successes).

## Prompt Structure

### System Prompt (execution-only agent)
- Role & identity
- Code execution mode instructions (MCP API access)
- Learn code mode rules:
  - Write main.py to `execution/{step-path}/code/`
  - Use `diff_patch_workspace_file` to update existing main.py (prefer over full rewrites)
  - May call tools via API to inspect state before writing
  - Use SCRIPT_VERBOSE for debug logging
  - Fallback: can use tools directly if unable to write main.py
- Python best practices (env vars, call_mcp helper with verbose logging)
- Error diagnostics (print to stdout, not just files)
- Validation schema (output requirements)
- Variables (env var mappings only, never actual values)
- Workspace paths, folder guard

### User Message
- Orchestrator instructions (task description)
- Prior script + error (if relearn — moved from system prompt for higher salience)
- Saved script reference (points to `execution/{step-path}/code/main.py`, NOT learnings/)
- Input dependencies
- Output requirements
- Execution checklist

### Fix Loop Feedback (injected as user messages)
Each fix iteration gets a self-contained feedback message:
- Task description
- Current main.py content (read from disk)
- Static code review issues (if any)
- Last execution output + exit code
- Validation errors (which files missing, which checks failed)
- Fix attempt counter

## Tier Selection

| Agent | Tier | Rationale |
|-------|------|-----------|
| Original execution | From context (preferredTier or maturity-based) | Normal selection |
| Repair agent (fix loop) | Tier 1 (High) forced | Needs stronger model to fix failures |
| Code exec fallback | Re-runs selection | Fresh agent, normal tier |

## Logging

### Prompt Logs (logs/agent_prompts/)
- `{seq}_{timestamp}_{agent}_{provider}_{model}.md` — system prompt + user message (at start)
- `{seq}_{timestamp}_{agent}_{provider}_{model}_conversation.md` — tool calls + responses (at end)

### Tool Call Events
- Collected via EmitTypedEvent (works for all providers including gemini-cli)
- Includes tool name, server, args, result, duration

### SCRIPT_VERBOSE
- Always set to "1" when controller runs scripts
- call_mcp helper logs: request args, response content, errors
- Scripts should use `VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'` for conditional debug output

## Key Source Files

| File | What |
|------|------|
| `controller_execution.go` | Main execution loop, retry logic, fix loop, code_exec fallback |
| `controller_learn_code.go` | `tryRunSavedLearnCodeScript`, `saveLearnCodeScriptToLearnings`, `reviewMainPyScript`, `GetLearnCodeModeInstructions`, `generateSimpleDiff` |
| `interactive_workshop_manager.go` | `isScriptedExecutionModeConfig`, mode config helpers |
| `execution_manager.go` | Execution context, saved-script-only mode |
| `controller_agent_factory.go` | Agent creation for execution and repair agents |
