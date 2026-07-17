# Workflow Monitoring

This doc covers the workflow observability surfaces that still exist in the product today.

It is worth keeping. The workflow UI still exposes execution logs, cost analysis, evaluation reports, learnings, and run history. What changed is the architecture behind them: older validation-heavy and per-step-learning explanations are no longer the right model.

## Monitoring Surfaces

There are three practical scopes:

### 1. Workflow-level views
- **Pulse**: the single agent-curated HTML log (`builder/improve.html`) — the primary at-a-glance surface (see below)
- **Costs**: aggregated token and USD usage across run folders
- **Evaluation reports**: benchmark-style scoring across runs, with single-run drill-down
- **Learnings**: current persisted learning state, including the global workflow skill

### 2. Run-folder views
- **Execution logs**: detailed logs for one selected run folder such as `iteration-12` or `iteration-12/group-a`
- **Final outputs**: generated final reports for a run folder

### 3. Cross-workflow operational views
- **Workflow overview**: recent run folders, status, timestamps, costs, and evaluation presence across workflows
- **Scheduled runs panel**: cron job history, latest runs, live sessions, and drill-down into logs/costs/evaluation for scheduled executions

## Pulse — the agent-curated log

The **Pulse** (`builder/improve.html`) is the primary workflow-level monitoring surface and the user's main window into a workflow. It is a single, self-contained HTML document the workflow's agents curate, rendered as a first-class right-panel view alongside Plan, Report, and Soul, and it follows the app's light/dark theme.

When pending input exists, Runloop renders **Needs your decision** first. Goal / Ikigai renders `soul/soul.md` directly. The HTML history is newest-first and time-based: active **Assumptions challenged** when needed, **Today's outcome**, collapsed technical detail, recent runs, attributed Signal / Reflection / Improvement cards, a collapsed **Agent log**, and archive. The Pulse popup uses each card's section/module metadata to show that same history by review. The Agent log contains only current handoff state, ids, cursors, cadence, and evidence pointers; it never duplicates the user narrative. Every workflow is judged on two independent axes, each stamped with the run it's as-of:

- **Bug** — did it run correctly (errors, skipped steps, missing/empty artifacts, regressions)? Fixed by hardening.
- **Goal** — is it achieving its success criteria (eval scores and outcome metrics vs `soul.md`)? Fixed by refining or replanning.

A **Pulse run** follows each scheduled workflow run. Pulse Gate selects only the due review modules, those reviewers return evidence without writing, and the parent Pulse Fixer applies bounded verified changes before the final dashboard/backup/publish/notify step. Enable it with the **Pulse** toolbar control. Current questions are stored as structured human-input requests and rendered by Runloop; answered question/outcome history is preserved under Reflection / Hansei.

## Execution Logs

Execution logs are still the main debugging surface for a single run folder.

Current behavior:
- The popup is opened from the workflow toolbar and is scoped to the currently selected run folder when possible.
- Backend data comes from `/api/workflow/logs`.
- The server scans `runs/{runFolder}/logs/` and `runs/{runFolder}/execution/`, then maps folder names back to plan step metadata from `planning/plan.json`.

The log viewer still supports these file families:
- `execution/execution-attempt-{A}-iteration-{I}.json`
- `execution/execution-attempt-{A}-iteration-{I}-conversation.json`
- `validation.json` and `validation-{N}.json`
- `learning-execution.json`
- `conditional-evaluation.json`
- `orchestration-execution.json`
- `todo-task-execution.json`

Important current nuance:
- validation logs still exist in the execution log viewer, but validation is no longer the main architecture story for workflow docs
- pre-validation remains relevant runtime signal, but the canonical validation doc is [pre_validation_guide.md](./pre_validation_guide.md)
- execution logs are best thought of as per-run forensic data, not as the source of workflow architecture truth

## Costs

Cost analysis is still current and useful.

Current behavior:
- The popup is opened from the workflow toolbar, overview page, and scheduled runs panel.
- Backend data comes from `/api/workflow/costs`.
- It aggregates token usage across run folders and supports drill-down into individual runs.
- The UI highlights the currently selected run folder when applicable.

The cost UI is still workflow-level, not step-config architecture.

## Evaluation Reports

Evaluation reports are still current and should stay documented.

Current behavior:
- Backend data comes from `/api/workflow/evaluation-reports`.
- The UI supports both **All Iterations** and **Single Iteration** views.
- The active run folder is highlighted as **Current** when present.

This remains a separate testing and benchmarking surface, not part of the learning or pre-validation model.

## Learnings

The learnings popup is still relevant, but the old explanation was stale.

Current behavior:
- Backend data comes from `/api/workflow/learnings/all`.
- The popup shows both step metadata and the global workflow learning entry `_global`.
- `_global` is rendered as **Workflow Knowledge (Global)** in the UI.
- Locking and unlocking still exist via step config updates.
- Deleting learnings for a step still exists.

What changed:
- learning is no longer best described as per-step prose memory with complexity caps and exploitation phases
- the main persistent learning surface is now the shared skill at `learnings/_global/SKILL.md`
- scripted steps can still have step-specific saved code such as `learnings/{step-id}/main.py`

The canonical architecture doc for this is [learning_architecture.md](./learning_architecture.md).

## Run Overview And Scheduled Runs

There are now two additional monitoring surfaces worth acknowledging:

### Workflow overview
- lists run folders from consolidated workspace state
- reconciles run metadata with active in-memory executions
- shows status, timestamps, cost summary, and evaluation presence across workflows

### Scheduled runs panel
- shows cron jobs and run history
- opens the same logs, costs, evaluation, and final-output popups for scheduled executions
- helps operational users inspect automation outcomes without opening each workflow canvas first

## Keep Or Remove?

Keep it.

This doc is still relevant because the product clearly has workflow monitoring and observability features. It just needed to stop documenting the older validation-era and complexity-cap model.

## Related Docs

- [cost_and_log_measurement.md](./cost_and_log_measurement.md)
- [pre_validation_guide.md](./pre_validation_guide.md)
- [learning_architecture.md](./learning_architecture.md)
- [evaluation_system.md](./evaluation_system.md)
- [workflow_manifest_architecture.md](./workflow_manifest_architecture.md)
