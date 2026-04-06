# Learn Code (Scripted Code) Execution Flow

## Overview

Learn code mode makes step execution **reproducible**. The LLM writes a `main.py` script that gets saved to `learnings/{step-id}/`. On subsequent runs, the saved script runs first (0 LLM tokens). If it fails, the LLM fixes it.

## Key Paths

| Path | Purpose | Persistence |
|------|---------|-------------|
| `learnings/{step-id}/main.py` | Saved script (best known version) | Persistent across runs |
| `execution/{step-path}/code/main.py` | Working copy (LLM writes/fixes here) | Preserved within a run, cleaned between runs |
| `execution/{step-path}/` | Step output files (STEP_OUTPUT_DIR) | Cleaned before each script run (code/ preserved) |
| `learnings/_global/SKILL.md` | Global skill file | Persistent |

## Execution Flow

### Phase 1: Saved Script (Fast Path)

```
tryRunSavedLearnCodeScript()
  1. Check: does learnings/{step-id}/main.py exist?
     - No  -> skip to Phase 2 (LLM generates from scratch)
     - Yes -> continue

  2. Check: does execution/{step-path}/code/main.py already exist?
     - Yes -> use it (may be LLM-fixed from previous attempt)
     - No  -> copy all files from learnings/{step-id}/ to execution/{step-path}/code/

  3. Clean output files in execution/{step-path}/ (preserve code/)

  4. Run main.py from execution/{step-path}/code/
     - STEP_OUTPUT_DIR = execution/{step-path}/
     - SCRIPT_VERBOSE=1 (always set for debug output)
     - All SECRET_*, VAR_*, MCP_API_URL, MCP_API_TOKEN injected as env vars

  5. Run pre-validation on outputs
     - Script exit 0 + validation passes -> SUCCESS (skip LLM entirely)
     - Script fails or validation fails -> Phase 2
```

### Phase 2: LLM Execution

```
executionAgent.Execute()
  - System prompt: code execution instructions, Python best practices, env var mappings
  - User message: task description, orchestrator instructions, prior script + error (if relearn)
  - LLM writes main.py to execution/{step-path}/code/main.py
  - LLM runs it, calls MCP tools via API, debugs as needed
  - LLM's turn ends
```

### Phase 3: Validation Fix Loop (up to 5 iterations)

```
After LLM's turn:
  1. Run pre-validation on outputs
     - Passes + main.py exists -> SUCCESS
     - Fails -> build feedback message:
       - Task description
       - Current main.py content
       - Validation errors (which files missing, which checks failed)

  2. Inject feedback as user message into SAME conversation

  3. Create repair agent (Tier 1/High) on first failure
     - Different model than original (may upgrade from gemini-cli to claude)
     - Same conversation history carried forward

  4. LLM fixes main.py, re-runs it

  5. Repeat validation check (back to step 1)

  6. After 5 failed attempts -> switch to normal code_exec mode (Phase 4)
```

### Phase 4: Fallback to Code Exec (outer retry)

```
If learn_code fix loop exhausted:
  - Disable learn_code mode for remaining retries
  - LLM uses tools directly (no main.py requirement)
  - Pre-validation still checks outputs
  - Up to 2 more attempts (3 outer retries total)
```

## Script Persistence

### When is execution/code/ -> learnings/ saved?

**Always after the fix loop**, regardless of success or failure. The rationale: the learnings script already failed, so any LLM-produced version is a newer attempt and likely better.

- Fix loop succeeds -> save to learnings (working script)
- Fix loop fails -> save to learnings (latest attempt, better than the known-broken version)
- Fast path succeeds -> save to learnings (may be LLM-fixed from previous run)

### When is execution/code/ cleaned?

- **Output files** (execution/{step-path}/): cleaned before each script run
- **Code directory** (execution/{step-path}/code/): **never cleaned** -- preserved across retries so LLM fixes survive
- Code directory is only cleaned when the outer workflow starts a fresh iteration

## Prompt Structure

### System Prompt (execution-only agent)
- Role & identity
- Code execution mode instructions (MCP API access)
- Learn code mode rules:
  - Write main.py to execution/{step-path}/code/
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
- Prior script + error (if relearn -- moved from system prompt for higher salience)
- Input dependencies
- Output requirements
- Execution checklist

### Fix Loop Feedback (injected as user messages)
- Task description (self-contained, survives provider changes)
- Current main.py content (read from disk)
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
- `{seq}_{timestamp}_{agent}_{provider}_{model}.md` -- system prompt + user message (at start)
- `{seq}_{timestamp}_{agent}_{provider}_{model}_conversation.md` -- tool calls + responses (at end)

### Tool Call Events
- Collected via EmitTypedEvent (works for all providers including gemini-cli)
- Includes tool name, server, args, result, duration

### SCRIPT_VERBOSE
- Always set to "1" when controller runs scripts
- call_mcp helper logs: request args, response content, errors
- Scripts should use `VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'` for conditional debug output
