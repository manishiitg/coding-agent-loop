## FILE LAYOUT

**Shell working directory**: the absolute workspace path (e.g. `/app/workspace-docs/Workflow/<name>/`) — get the exact value from the CURRENT STATE block of your system prompt or from `AbsWorkspacePath` if available.

- Always use **absolute paths** in shell commands: prefix every path with the workspace root.
- Do **not** use `cd` or relative paths.

All paths below are relative to the workspace root (prepend the absolute root when running shell commands).

### Plan & Config
| Path | Contents |
|------|----------|
| planning/plan.json | Workflow plan — step definitions, descriptions, validation schemas |
| planning/step_config.json | Step-level config overrides (LLM, execution mode, learnings, etc.) |
| reports/report_plan.json | Dynamic report widget definitions (see persistent-stores design) |
| soul/soul.md | Canonical workflow north star: objective and success criteria |

### Execution Outputs (per run, per group)
| Path | Contents |
|------|----------|
| runs/{iter}/{group}/execution/{step-id}/ | Step output files (*.json) |
| runs/{iter}/{group}/execution/Downloads/ | Downloaded files (bank statements, etc.) |
| costs/execution/{group}/{YYYY-MM-DD}.json | Execution token usage ledger for that group/day |
| costs/phase/token_usage.json | Aggregated phase-only token usage |

### Execution Logs (per run, per group, per step)
| Path | Contents |
|------|----------|
| runs/{iter}/{group}/run_metadata.json | **Workflow-level timing**: `started_at`, `completed_at`, `duration_ms`, `status` |
| runs/{iter}/{group}/logs/{step-id}/execution/*-conversation.json | Full conversation log: `conversation_history` (messages) + `tool_calls[]` (each with `tool_name`, `args`, `result`, `duration`) |
| runs/{iter}/{group}/logs/{step-id}/execution/*-iteration-*.json | Execution summary: model, result text, step path, `duration_ms`, `llm_call_count`, `llm_duration_ms`, `tool_call_count`, `tool_duration_ms` |
| runs/{iter}/{group}/logs/{step-id}/execution/*-timing.json | **Clear timing breakdown**: read `agent.*` for agent wall-clock, `llm.*` for LLM timing (`time_to_first_response_ms`, `time_to_first_content_ms`, `time_to_first_tool_call_ms`), and `tools.calls[]` for per-tool durations/offsets |
| runs/{iter}/{group}/logs/{step-id}/execution/scripted_fast_path.json | **scripted steps**: main.py result — `exit_code`, `output` (stdout), `error`, `success`, `script_path` |
| runs/{iter}/{group}/logs/{step-id}/pre_validation.json | Pre-validation result: `overall_pass`, `errors[]`, `files_checked[]`, `schema_used` |

### Best Way To Read Timing

Use this order when debugging latency:

1. Read `run_metadata.json` first to get the total workflow wall-clock and whether the run finished or failed.
2. Read each step's `execution-attempt-{N}-iteration-{M}.json` next to rank slow steps quickly using `duration_ms`, `llm_duration_ms`, and `tool_duration_ms`.
3. Open the matching `execution-attempt-{N}-iteration-{M}-timing.json` for the slowest step.
4. In that timing file, interpret fields in this order:
   - `agent.duration_ms` = full wall-clock time for the step attempt.
   - `llm.total_duration_ms` = total time spent waiting on LLM calls across the attempt.
   - `llm.time_to_first_response_ms` = delay before the model produced its first visible response signal.
   - `llm.time_to_first_content_ms` = delay before the first text content arrived.
   - `llm.time_to_first_tool_call_ms` = delay before the model decided to invoke a tool.
   - `tools.total_duration_ms` = total time spent inside tools.
5. Use `llm.calls[]` to see whether one LLM call dominated latency or whether many smaller calls accumulated.
6. Use `tools.calls[]` to find the exact slow tool. Prefer `duration_ms` for cost/time ranking and `offset_from_agent_start_ms` to understand when it happened inside the step.
7. If `agent.duration_ms` is much larger than both `llm.total_duration_ms` and `tools.total_duration_ms`, infer the remaining gap is orchestration overhead, prompt construction, validation, file IO, or other non-LLM/non-tool work.
8. Use the conversation log only after timing isolation, to explain *why* the slow LLM/tool call happened rather than to discover *which* one was slow.

### Learnings (persistent across runs)
| Path | Contents |
|------|----------|
| learnings/{step-id}/main.py | **scripted steps**: saved Python script — executed on each scripted run via fast path |
| learnings/_global/SKILL.md | Global prose learnings shared across all steps |
| learnings/{step-id}/script_metadata.json | Script version, run counts, per-group stats, duration stats, recent run history (last 10 with exit codes/errors/durations), last failure details, success/failure streak |

### Evaluation
| Path | Contents |
|------|----------|
| evaluation/evaluation_plan.json | Eval step definitions |
| evaluation/runs/{iter}/{group}/evaluation_report.json | Eval step outputs + evidence |

### Other
| Path | Contents |
|------|----------|
| builder/session-{id}-conversation.json | Previous builder chat sessions |
| db/*.json | Workflow state and results (JSON rows produced by steps; upsert-by-key; see persistent-stores design) |
| db/assets/* | Durable media/file assets referenced by db rows, report widgets, or later steps |
| knowledgebase/context/context.md | User-supplied runtime business context that steps with KB read access must respect |
| knowledgebase/notes/*.md | Per-topic narrative markdown — durable observations discovered by the workflow. Normally written by step agents in direct-write mode; post-step KB agent only when explicitly requested. |
| knowledgebase/notes/_index.json | Topic registry (covers, size_bytes, section_count, last_updated) kept in sync with notes/*.md |

**Cleanup**: Delete old builder conversation files when >3 exist (`ls -t builder/session-*.json`, keep latest).
