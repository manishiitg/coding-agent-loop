# Learn Code and Code Execution Modes

This is the current source of truth for scripted workflow execution.

`learn_code` and `code_exec` are not separate systems. They are two execution modes built on the same code-execution foundation:

- `code_exec`: the agent writes and runs code for the current run only.
- `learn_code`: the agent writes and maintains a reusable `main.py` that is retried on future runs before the LLM is called.

## Overview

At the workflow step level, scripted execution is controlled by two fields in `agent_configs`:

- `use_code_execution_mode`
- `declared_execution_mode`

The current behavior is:

| Setting | Effect |
|---|---|
| `use_code_execution_mode: false` | Step uses normal direct-tool execution, not scripted code execution |
| `use_code_execution_mode: true` + `declared_execution_mode: "code_exec"` | Step uses ephemeral code execution only |
| `use_code_execution_mode: true` + `declared_execution_mode: "learn_code"` | Step uses persistent scripted execution with saved `main.py` fast path |

Important implementation detail:

- `learn_code` is detected only when `declared_execution_mode == "learn_code"`.
- `code_exec` is the fallback mode whenever scripted execution is enabled but the step is not explicitly marked as `learn_code`.
- `syncDeclaredExecutionModeConfig()` forces `use_code_execution_mode=true` when `declared_execution_mode` is `learn_code` or `code_exec`.

That means the recommended workflow config is to set both fields explicitly for every optimized scripted step.

## Recommended Usage

Prefer `learn_code` for most stable workflow steps:

- structured data transforms
- report building
- deterministic validation logic
- fixed API call sequences
- repeatable file processing
- browser flows with stable selectors and predictable navigation

Use `code_exec` when the step still benefits from scripting, but the exact logic changes from run to run:

- exploratory browser work
- adaptive investigations
- tasks where the agent must improvise heavily based on page state or live results
- one-off data collection patterns that are unlikely to stabilize into a reusable script

## Configuration

### Preferred `learn_code` config

```json
{
  "id": "step-id",
  "agent_configs": {
    "use_code_execution_mode": true,
    "declared_execution_mode": "learn_code",
    "declared_execution_mode_reason": "Stable scripted flow with reusable Python"
  }
}
```

### Ephemeral `code_exec` config

```json
{
  "id": "step-id",
  "agent_configs": {
    "use_code_execution_mode": true,
    "declared_execution_mode": "code_exec",
    "declared_execution_mode_reason": "Adaptive step that changes between runs"
  }
}
```

### Workshop defaults

Workshop guidance treats `learn_code` as the preferred default for optimized scripted steps. The workshop tools also expose:

- `update_step_config(...)`
- `run_saved_main_py(step_id, group_id?)`

`run_saved_main_py` is valid only for `learn_code` steps, because only those steps have a persistent saved-script fast path.

## Shared Architecture

Both modes use the same bridge-based execution model.

The execution agent does not call most MCP tools directly. Instead it:

1. Uses `get_api_spec(server_name, tool_name)` to inspect a tool's HTTP contract.
2. Uses `execute_shell_command` to write and run Python or shell code.
3. Calls per-tool HTTP endpoints such as:
   - `POST /tools/mcp/{server}/{tool}`
   - `POST /tools/custom/{tool}`

Core env vars injected into scripted runs include:

- `MCP_API_URL`
- `MCP_API_TOKEN`
- `STEP_OUTPUT_DIR`
- `STEP_EXECUTION_DIR`
- resolved `SECRET_*` and `VAR_*` values

This is the same bridge used by CLI-style providers that require HTTP tool routing.

## Mode Resolution and Precedence

The execution loop resolves mode in two layers:

1. Determine whether the step is in persistent scripted mode:
   - `isScriptedExecutionModeConfig(cfg)` returns true only for `declared_execution_mode == "learn_code"`.
2. Determine whether code execution is enabled at all:
   - step config `use_code_execution_mode`
   - otherwise workflow/preset default
   - then `learn_code` forces code execution on

Additional behavior:

- Step config overrides workflow default.
- Workflow default no longer auto-enables code execution globally.
- Provider-specific auto-enable is handled per agent for CLI providers such as `claude-code`, `pi-cli`, and `codex-cli`.

## `learn_code` Flow

`learn_code` adds persistence and a saved-script fast path on top of normal code execution.

### Persistent paths

| Path | Purpose |
|---|---|
| `learnings/{step-id}/main.py` | Canonical saved script for future runs |
| `learnings/{step-id}/diffs/` | Diffs between saved versions |
| `execution/{step-path}/code/main.py` | Per-run working copy that the LLM edits |
| `execution/{step-path}/code/fix-diffs/` | Diffs between repair iterations in the same run |
| `execution/{step-path}/` | Output folder for artifacts validated by the step |

### Fast path

Before the LLM runs, the controller attempts `tryRunSavedLearnCodeScript(...)`.

High-level flow:

1. Check whether `learnings/{step-id}/main.py` exists.
2. Run static review on the saved script.
3. Copy the saved script into `execution/{step-path}/code/` when needed.
4. Clean the step output directory while preserving `code/`.
5. Run `python3 main.py` with workflow env vars and step arguments.
6. Run pre-validation on outputs.
7. If script execution and validation pass, finish with zero LLM tokens for that run.

### Static review before fast path

The controller reviews the saved script before trusting it. It rejects fast path when it sees patterns such as:

- hardcoded execution paths
- hardcoded fallbacks for required env vars
- sibling-step path hacks
- writes outside the managed step output area
- direct writes into system-managed directories like `knowledgebase/` or `learnings/`

When static review fails, the system skips the fast path and falls back to LLM repair/generation.

### LLM generation and repair

If fast path fails or no saved script exists:

1. The execution agent writes or repairs `execution/{step-path}/code/main.py`.
2. The controller reruns pre-validation.
3. On failure, it starts a learn-code repair loop.

Repair loop behavior:

- up to 3 fix iterations (configurable via `LearnCodeMaxFixIter`)
- fresh Tier 1 (High) repair agent each iteration
- feedback message includes: task description, pointer to current `main.py` on disk (not inlined), static code review issues, last execution output + exit code, and attempt counter
- validation details are intentionally omitted from feedback to prevent the LLM from fabricating outputs that match the schema
- diffs are written under `execution/{step-path}/code/fix-diffs/`

### Save-back behavior

After learn-code execution, the controller saves the latest script back into `learnings/{step-id}/` unless:

- the script has syntax errors (definitely worse than the saved version)
- `lock_learnings` is true (user has frozen the script intentionally)

This means `learn_code` is not only a fast path. It is also the persistent script-maintenance path.

### Lock code vs lock learnings

There are two separate locks plus the access-level gate:

| Setting | Controls | Effect |
|---|---|---|
| `learnings_access` (`"read"\|"read-write"\|"none"`) | SKILL.md read/write at a coarse level | Default `"read"` — step sees `_global/SKILL.md` but doesn't contribute. `"read-write"` (+ non-empty `learning_objective`) opts into contribution. `"none"` opts out of both. Mirrors `knowledgebase_access`. |
| `lock_learnings: true` | SKILL.md writes | Freezes the learning agent for this step. Existing SKILL.md still flows into execution prompts. **Auto-set after 3 successful runs against the same step-description hash; auto-cleared on description change** for steps that were auto-locked (manual locks stay put). |
| `lock_code: true` | main.py | Prevents LLM-rewritten scripts from being saved back to learnings. Skips the fix loop entirely (falls back directly to code_exec mode). |

When `lock_code: true` is set on a step:

- **Fast path**: Saved script is still copied from learnings to execution and run normally
- **Fix loop**: Skipped entirely (`maxFixIter = -1`) — no repair agents are created, no tokens spent on fixes that would be discarded
- **Save-back**: Blocked — the LLM's rewritten script is NOT copied back to learnings
- **Fallback**: Falls through directly to code_exec mode (tools directly, no main.py)
- **Metadata**: `script_metadata.json` is still updated (run history, failure patterns) for observability

This means a locked script that keeps failing will repeat the same failure every run. The user must manually fix `learnings/{step-id}/main.py` or set `lock_code: false` to let the system fix it.

To force a complete rewrite: delete `learnings/{step-id}/main.py` (not the execution copy), then run `execute_step`. The LLM will generate fresh.

### Fallback after repair exhaustion

If the learn-code repair loop is exhausted (or skipped due to locked learnings), the controller disables persistent scripted mode for the remaining outer retries and continues in plain `code_exec` mode.

That fallback is important:

- `learn_code` is the preferred stable path
- `code_exec` is the recovery path when the saved script is not currently salvageable within the repair budget

## `code_exec` Flow

`code_exec` uses the same bridge and env model, but it does not rely on a persistent saved script.

Behavior:

- the agent writes and runs code for the current step run
- no saved `learnings/{step-id}/main.py` fast path is attempted
- no `run_saved_main_py` support
- the step still benefits from script-based batching, loops, parsing, and multi-tool orchestration

This is the correct mode when scripting is useful but persistence would create more churn than value.

## Prompting Expectations for Scripted Steps

The controller prompt for scripted execution expects:

- outputs to be written under `STEP_OUTPUT_DIR`
- script working files to live under `STEP_EXECUTION_DIR` / `code/`
- variables to be passed through env vars or runtime args, not hardcoded
- diagnostic output to go to stdout/stderr so repair loops can reason over failures

For `learn_code`, the prompt also emphasizes:

- maintaining a reusable `main.py` and repairing it incrementally
- **no fabricated data**: every output value must trace to a real data source (MCP tool call, API response, or input file)
- **browser automation rules**: snapshot-first agent_browser interaction, fresh refs, and durable persisted selectors
- **tool discovery**: call `get_api_spec` before writing browser/MCP code to learn exact parameter schemas instead of guessing
- `script_metadata.json` is referenced by path (not inlined) so the LLM reads it on demand

## When to Use Which Mode

Choose `learn_code` when:

- the task shape is stable
- the script should improve over time
- you want future runs to be cheap and fast
- you want a reviewable `main.py` artifact in learnings

Choose `code_exec` when:

- the task shape changes too much between runs
- persistence would encode brittle assumptions
- the agent needs exploratory or dynamic behavior each time

## Operational Notes

- CLI providers may force code execution behavior because they route tools through the HTTP bridge.
- `learn_code` steps force `UseCodeExecutionMode = true` regardless of provider — this ensures the agent gets the tool index and `get_api_spec` virtual tool for proper tool discovery when writing `main.py`.
- Learning agents are still separate from execution agents; code execution mode mainly affects execution-time tool access and scripting behavior.
- `learn_code_script_execution` events exist specifically for saved-script runs and repair visibility in the UI.
- `error_summary` in `script_metadata.json` run records is stored in full (not truncated). `error_snippet` in `last_failure` is capped at 2000 chars for prompt inclusion.

## Key Files

| File | Role |
|---|---|
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go` | Main execution loop, fast-path invocation, repair loop, fallback handling |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learn_code.go` | Saved-script execution, static review, save-back, diff capture |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go` | Mode semantics, workshop guidance, `run_saved_main_py`, config sync helpers |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/step_config.go` | Applies step config and syncs declared mode to boolean flags |
| `agent_go/cmd/server/server.go` | Per-tool HTTP endpoints and bridge env setup |
| `agent_go/pkg/workspace/execute_shell_command.go` | Shell execution guardrails and tool-routing constraints |

## Related Docs

- [Step Config Specification](step_config_format_specification.md)
- [Tool Search Mode](../core/tool_search_mode.md)
- [Learning Architecture](learning_architecture.md)
