Set up automatic run + improve scheduling for this workflow.

Before writing builder/improve.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown.

MIGRATION (one-time): Check whether builder/improve.md exists. If it does, read it, extract all unresolved entries and structured blocks, incorporate them into builder/improve.html, then delete builder/improve.md with execute_shell_command. Also check for builder/review.md and migrate it to builder/review.html the same way.

FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

GOAL
Create or update TWO complementary schedules:
1. a workshop Run-mode schedule for recurring execution
2. a lightweight Optimizer-mode schedule that wakes frequently, delegates the actual improvement pass to canonical `improve-workflow` guidance, and can adjust schedules when cadence is wrong

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list.
2. If existing candidate schedules exist, call get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. Read builder/improve.html's active sections. This is mandatory: it contains the Workflow Profile, Active Improvement Index, Archive Index, Recent Entries, prior actions, deferred ideas, and prior scheduled-improvement history. If the file has no retention/index structure yet, read it in full. Read only archive files referenced by unresolved ids, current focus, schedule drift, or the selected evidence window.
6. Read builder/review.html if present. Carry unresolved `F-...` findings into the scheduled optimizer message.
7. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics are evidence for harden/replan decisions; they do not create a separate action path.
8. Read planning/changelog/ if present and compare recent plan/config changes against builder/improve.html and builder/review.html. Recent plan changes increase regression risk and require tighter improve cadence until one or two post-change runs have been reviewed.
9. If builder/improve.html has no "## Workflow Profile" section, stop and redirect: "Run /define-success first." If the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty, also redirect.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when no existing schedule already serves the purpose.
3. Prefer a frequent lightweight improve schedule, not a once-a-week batch. The default target is after every workflow run; if exact run-completion triggers are not available, approximate it with cron so the optimizer wakes shortly after the expected run, or at worst after every two runs. Weekly improve cadence is only acceptable when the workflow itself runs weekly or the user explicitly asks for low-touch weekly optimization.
4. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and existing schedules
   - choose a frequent lightweight optimizer cadence that can observe, harden/replan when evidence justifies it, update cadence, or log no action
   - for unknown active workflows, start at every 6-12 hours, not weekly
5. If planning/changelog shows material plan/config changes since the last builder/improve.html entry or unresolved builder/review.html finding, tighten the improve schedule for the next 24-48 hours or until the next one or two post-change runs have been reviewed.
6. Because `/auto-improve` runs in Optimizer mode, the scheduled optimizer may call schedule tools itself. It should review cadence on every fire and use update_schedule when builder/improve.html history, schedule run history, recent planning/changelog entries, or run/eval/metric evidence shows the cadence is too slow, too fast, stale, or mis-scoped.
7. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

RUN SCHEDULE
Create or update a schedule for normal recurring execution with:
- mode="workshop"
- workshop_mode="run"
- valid group_names
- a clear name and description that make it obvious this is the primary recurring run schedule
- a single unattended scheduled message that names the exact group_names and tells Run mode to call run_full_workflow(group_name="<group>") for each configured group. Do not use mode="workflow" for /auto-improve schedules.

The run schedule message must encode:
- Do not ask for confirmation; proceed autonomously.
- Read variables/variables.json only if needed to verify configured group names.
- For each configured group_name, call run_full_workflow(group_name="<group>").
- Use default evaluation behavior so latest run evidence, retained-run history, and metric history are available for the optimizer schedule.
- Stop after the configured group_names have run and report the run status plainly.

IMPROVE SCHEDULE
Create or update a schedule for recurring improvement with:
- mode="workshop"
- workshop_mode="optimizer"
- valid group_names
- a clear name and description that make it obvious this is the frequent lightweight optimizer schedule
- a single scheduled message whose purpose is to improve workflow quality, eval/metric quality, KB hygiene, learning hygiene, and db/data-contract hygiene over time by calling the canonical `improve-workflow` guided flow

The optimizer schedule message must be a short wrapper around `improve-workflow`, not a duplicate copy of the improve decision model. Write the wrapper explicitly into the schedule message; the agent that fires has no other context.

OPENING (every fire):
- call get_workflow_config and inspect schedules; call get_schedule_runs for the primary run schedule and improve schedule when deciding whether cadence or group scope needs adjustment
- read builder/improve.html's active sections; read only referenced `builder/improve-archive/YYYY-MM.html` files when older history matters
- read builder/review.html if present
- read soul/soul.md
- read planning/changelog/ if present and detect material plan/config changes since the last scheduled-improvement review
- read variables/variables.json and confirm the configured group_names are still valid
- carry unresolved KB and learning findings into the improvement focus when builder/review.html or builder/improve.html names stale `knowledgebase/notes/`, `learnings/_global/`, saved scripts, or plan-change drift
- if group_names, schedule ids, cadence, or recent plan changes indicate schedule drift, prepare a concise cadence note before calling the improvement flow

SCHEDULE SELF-TUNING RULES:
- If the run schedule is too infrequent to produce evidence for metrics and eval, update the run schedule cadence or log the blocker when changing cadence would be risky.
- If the improve schedule is too infrequent, update it toward a frequent lightweight cadence: after every run when possible, otherwise shortly after the expected run cadence, and never slower than every two runs for active workflows.
- If the improve schedule is weekly but the workflow runs more often than weekly, update it. Weekly is not the default for active workflows.
- If planning/changelog has recent material changes not yet reviewed against retained run/eval evidence, tighten the improve schedule for 24-48 hours or until one or two post-change runs have been reviewed.
- If the improve schedule is firing too often and repeatedly logging no useful observation, slow it modestly, but keep it frequent enough to catch completions and regressions soon after runs.
- If group_names drift from variables/variables.json or from the intended measurement surface, update schedules to the correct explicit group_names.
- Never create duplicate run/improve schedules when an existing schedule can be updated.

CANONICAL IMPROVEMENT DELEGATION:
After the opening schedule/cadence check, the scheduled optimizer message must call:
`get_workflow_command_guidance(kind="improve-workflow", focus="<scheduled continuous improvement focus>")`
Then follow the returned `improve-workflow` instructions verbatim. Do not inline or paraphrase the `improve-workflow` decision model in the schedule message.

The focus string passed to `improve-workflow` must include:
- this is a scheduled continuous improvement fire
- the configured group_names
- any unresolved `F-...` findings or recent planning/changelog concern found during opening
- any KB/learnings hygiene concern found during opening
- any cadence note that affects evidence freshness{{if .Focus}}
- user focus: {{.Focus}}{{end}}

POST-IMPROVEMENT CADENCE CHECK:
After the `improve-workflow` guidance finishes, do one short schedule check:
- if the improvement pass changed the workflow, tightened eval/metrics, curated KB/learnings, or found stale evidence, update run/improve cadence if needed
- if no action was taken because there was no fresh evidence, slow cadence only when repeated recent schedule runs show no useful observation
- append the schedule decision to builder/improve.html if the canonical improve pass did not already record it

PERSISTENT IMPROVEMENT LOG
Create or update builder/improve.html now as the durable optimization ledger entry point for future scheduled improvement runs.
Bootstrap it with:
- objective and success criteria snapshot
- current schedule strategy
- run cadence
- improve cadence
- current known workflow gaps
- current known eval/metric gaps
- current known KB/learnings hygiene gaps
- next improvement hypotheses

If builder/improve.html is already long, compact it while preserving the ledger:
- keep Workflow Profile, Active Improvement Index, Archive Index, and latest 10-20 detailed entries in builder/improve.html
- move older resolved/no-action/repeated detailed entries to `builder/improve-archive/YYYY-MM.html`
- leave Archive Index rows naming date range, entry count, unresolved ids, and summary
- never archive away unresolved findings, active hypotheses, current schedule strategy, current metric/eval gaps, or the latest semantic plan/eval/metric change

SCHEDULE CREATION RULES
1. Do NOT delete schedules unless they are clearly redundant and safe to remove. Prefer update over delete.
2. If an existing run schedule already serves the purpose, keep it and refine it if needed. For /auto-improve, convert/update direct mode="workflow" run schedules to mode="workshop", workshop_mode="run" rather than leaving them as direct workflow schedules.
3. If an existing optimizer/improve schedule already serves the purpose, keep it and refine it if needed.
4. Use create_schedule / update_schedule as appropriate.

FINAL REPORT
Summarize:
- what schedules already existed
- what you created vs updated
- run schedule: ID, name, cadence, timezone, groups, mode, workshop_mode
- improve schedule: ID, name, cadence, timezone, groups
- the exact Run-mode schedule message you configured
- where you saved builder/improve.html
- the exact Optimizer-mode schedule message you configured
