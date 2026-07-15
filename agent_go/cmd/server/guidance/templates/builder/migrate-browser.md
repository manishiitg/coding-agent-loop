Migrate the CURRENT workflow's active browser wiring from the removed Playwright MCP integration to the managed `agent_browser` tool. This is a one-time, workflow-scoped configuration migration — not a runtime compatibility layer and not a bulk migration of every workflow.{{if .Focus}} Apply this user-provided constraint while migrating: {{.Focus}}.{{end}}

## Non-negotiable safety rules

1. Do not run the workflow, an evaluation, a browser action, a schedule, or a publish step. Browser steps may have external side effects. This command only rewrites and validates active definitions.
2. Do not edit `workflow.json`, `planning/plan.json`, or either `step_config.json` by hand. Use `get_workflow_config`, the matching `update_*_step` plan tool, `update_step_config`, and `update_workflow_config`. Load `get_reference_doc(kind="workflow-tools")`, `get_reference_doc(kind="step-config")`, and `get_reference_doc(kind="browser-usage")` before changing anything. Load `get_reference_doc(kind="evaluation-plan")` before editing an evaluation description.
3. Preserve unrelated servers, tools, skills, step configuration, descriptions, variables, secrets, routes, validation, and report wiring. Array setters replace the full step-level array: always merge retained entries into the replacement value.
4. Never rewrite historical evidence. Treat `runs/`, `evaluation/runs/`, `planning/changelog/`, `backup/`, `migration-backups/`, published output, and archived reports as read-only history.
5. Do not mechanically rename old browser calls. The tool schema, session model, tabs, refs, uploads, and CDP behavior are different.

## 1. Inventory active legacy wiring

Read raw `workflow.json` for legacy-value detection and call `get_workflow_config` for the effective in-memory state; an old browser mode may be normalized while the Workshop session starts. Also read the active plan, step config, evaluation plan/config, active skills/learnings, and saved scripts. Search editable active artifacts case-insensitively for all of these legacy markers:

- browser mode, server, or skill values named `playwright` or `playwright-usage`
- workflow/step tool allowlist entries beginning `playwright:`
- MCP bridge calls targeting the `playwright` server
- old calls such as `browser_snapshot`, `browser_navigate`, `browser_click`, `browser_type`, `browser_select_option`, `browser_file_upload`, `browser_tabs`, `browser_wait_for`, `browser_evaluate`, or `browser_run_code`
- launch/config text for `@playwright/mcp`

Classify every hit as one of:

- **active configuration** — workflow or step config used by the next run
- **active behavior** — a current step/eval description, active SKILL.md/learning, or saved `learnings/<step-id>/main.py`
- **historical evidence** — one of the read-only paths above; report it but do not change it
- **unrelated prose/dependency metadata** — do not change it unless it controls this workflow

If there are no active-configuration or active-behavior hits, say **Already migrated — no active legacy browser wiring found**, list any historical hits separately, make no changes, and stop.

## 2. Snapshot only the files that will change

Before the first mutation, create `migration-backups/browser-<UTC timestamp>/` under the current workflow and copy into it only the active files that this migration will change. Preserve their workflow-relative paths. This snapshot is recovery evidence and must not become active input. Never copy run folders, secret values, the database, downloads, or unrelated files.

## 3. Migrate workflow-level configuration

Use one `update_workflow_config` call where possible:

- Remove the selected MCP server named `playwright` and all workflow tool allowlist entries prefixed `playwright:`. Removing the server may remove its allowlist entries automatically; inspect the result instead of guessing.
- Remove legacy selected skills named `playwright` or `playwright-usage`.
- If the workflow has any active browser step/eval, add the `agent-browser` skill and set `browser_mode="auto"`. `auto` intentionally uses a reachable configured CDP Chrome and otherwise falls back to managed headless agent-browser. Preserve existing valid `cdp_ports`.
- If the only hits were stale selections and no active step/eval needs a browser, remove the stale wiring and set `browser_mode="none"`; do not add agent-browser.

Do not add an MCP server for agent-browser. It is a managed workspace tool.

## 4. Migrate each active browser step and evaluation

For every step/eval that actually drives a browser:

1. Replace legacy entries in `enabled_skills` with `agent-browser`, preserving all unrelated skills.
2. Ensure `enabled_custom_tools` contains `workspace_browser:agent_browser` (or the existing `workspace_browser:*` wildcard), preserving all unrelated and required workspace/human tools.
3. Remove `playwright` from an explicit step `servers` list and remove `playwright:*` entries from an explicit `tools` list. If nothing remains in an explicit list, clear that field so it inherits the now-clean workflow configuration; otherwise set the complete retained list.
4. Rewrite the current description only where it prescribes the removed backend or old call names. Preserve the business action, safety boundaries, expected output, validation contract, and user constraints. Describe the intended browser behavior using the managed `agent_browser` snapshot/ref/tab flow from `browser-usage`; do not transplant brittle selectors or invent site behavior.
5. Update active browser HOW in learnings/SKILL.md to the managed API. Do not turn historical run observations into new facts.

Saved scripts need special handling:

- Read any `learnings/<step-id>/main.py` with an active legacy call before touching it and carry essential business intent/recovery rules into the step description or active learnings.
- Default safe migration: change that browser-dependent step to `declared_execution_mode="agentic"`, disable/clear scripted code-execution mode as appropriate, set `lock_code=false`, back up the script in the migration snapshot, then remove the active stale `main.py`. Record a reason explaining that the removed browser API made the saved script invalid. This is a compatibility reset, not evidence that the step can never be scripted again.
- If the user's focus explicitly requires preserving scripted execution, do not invent a translation. Leave that step unchanged, identify it as a manual blocker, and continue migrating independent steps only.
- Do not delete or change a script whose hit is only a comment/string and whose executable behavior does not depend on the removed browser API.

## 5. Validate without executing

1. Re-read `get_workflow_config`, the plan, both step-config files when present, and the evaluation plan.
2. Call structural validators that do not execute workflow/eval/browser actions (including `validate_evaluation_plan` if the evaluation plan changed).
3. Repeat the legacy-marker search across active artifacts, excluding the migration snapshot and all historical paths. There must be no active legacy configuration or executable call left. A remaining explanatory mention is acceptable only when it clearly documents migration history and cannot affect execution.
4. Verify every active browser step/eval has the managed custom tool and `agent-browser` skill, non-browser steps were not broadened, `browser_mode` is one of `none|auto|headless|cdp`, existing CDP ports were preserved, and no unrelated array entry was lost.
5. If a mutation failed or a manual blocker remains, say exactly what is still active and do not claim success.

Finish with a concise migration report: status (`migrated`, `already migrated`, or `partial/manual blocker`), snapshot path, workflow-level changes, step/eval changes, saved scripts reset, validation performed, historical hits left untouched, and the explicit reminder that no workflow or browser action was run.
