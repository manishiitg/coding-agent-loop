# Pulse Goal Work: doing work for the user's goals

Status: **implemented, all phases** (2026-09-23): Phase 0 in `2d002e061`,
Phases 1–4 in the following commit. Live verification on a real Pulse run is
still pending. Supersedes the role split in PLAT-303/305 for Strategy.

### As built (differences from the design below)

- Goal Work items and constraint challenges live in their own workflow table,
  `pulse_goal_work`, written by one tool, `record_pulse_goal_work`, and read by
  `get_pulse_state(view="goal_work")`. They are not `pulse_issues` rows: their
  lifecycle (idea / in progress / needs user / done / dropped, then an effect)
  does not fit the bug lifecycle.
- The storage identity (`strategic_review`) and the guidance kind name
  (`strategy-auditor`) are unchanged; only their content, permissions and UI
  label changed. `templates/system/strategy-auditor.md` is the Goal Work
  contract; `templates/review/strategy-auditor.md` is the manual pass.
- Permissions: `background_review_scope.go` gives Goal Work the research tools
  plus `record_pulse_goal_work`, writes to `runs/pulse/<run>/` and
  `pulse/work/`, and `execute_step` / `run_full_workflow` only when
  `pulse.autonomy.run` is `auto` (the default) and no Plan Drift is due.
  Plan, schedule and dispatch tools stay withheld. Outward actions are a prompt
  contract: the workflow's own MCP and browser access cannot be filtered per
  action. The manual pass in Builder chat is not runtime-restricted and holds
  the levels by contract.
- Order: Plan Drift → Goal Work → Architecture → Technical
  (`pulsemodules.ExecutionOrder`). Only Goal Work runs while Drift is due.
- Constraint provenance and class are written by `/setup-goals` as
  `(user request, date)` and `[boundary]` / `[choice]` markers. The UI shows a
  constraint's class on its challenge; the Goals & rules panel is the existing
  soul summary and does not tag each constraint yet.
- Step concerns: steps end with a plain `CONCERNS:` line per consequential
  non-fatal problem (scripted steps print it to stdout). Go collects the lines
  written since the previous Pulse started (`pulse_step_concerns.go`) and hands
  them to Pulse in `get_pulse_state(view="module").step_concerns` and
  `view="step_concerns"`. The Gate, Goal Work and Technical use them as leads;
  nothing turns them into findings automatically. The old `record_run_concern`
  tool and Go harvester are removed.
- UI: `PulseWorkspace` has **For you** (goal summary, goal progress, Needs you,
  `PulseGoalWork`: Did for you, Challenging your rules, Next up, Pulse
  permissions) and **Platform health** (Drift, Technical, Architecture,
  Open/Closed maintenance issues, run history). `PulseImprovements` (the
  retired impact ledger) was removed.

## Why

Pulse exists to help the user reach their goals by doing work they are not doing,
or do not know they should do. Today it mostly maintains the workflow machine:

- 1,804 `pulse_issues` across 9 local workflows: 1,627 `workflow_issue`, 177
  `harness_issue`; only 4 are linked to `strategic_review`.
- Strategic reviews mostly end in `evidence_wait`, "handed off to engineering",
  or "Deferred until the due Plan Drift pass".
- Gate, fixer, Drift, Technical and Architecture prompts total about 1,700 lines;
  `strategy-auditor.md` is 311.
- Strategy is forbidden to act ("Never … run producing actions") and may only
  write to its Pulse run folder (`researchReviewToolAllowed`). Its best output
  is an approval card.
- `soul.md` `## Constraints` are injected as BINDING everywhere. Strategy is told
  to respect them, never to question them, although many were written by the
  builder or are old choices that may be limiting the goal.

## What changes

Strategic Review becomes **Goal Work**, Pulse's primary loop. Plan Drift,
Technical and Architecture become **platform upkeep** that runs in the
background and never blocks Goal Work.

The storage identity stays `strategic_review` (no PLAT-326 migration churn).
Only the UI label, prompt and permissions change.

### The Goal Work loop (one pass)

One retained background executor, run as a message sequence:

1. **Orient.** Goals and priorities from `soul.md`, `get_goal_metrics`, the last
   Goal Work result, answered decisions, and recent user feedback.
2. **Find the gap.** Ask: *if every step ran perfectly, what would still stop the
   primary metric from moving?* Look for three kinds of gap:
   - **Undone work:** things that would move the goal that nobody is doing
     (a missing channel, follow-up, audience segment or content type).
   - **Unknown to the user:** external research on what works in this domain
     (benchmarks, competitors, platform changes, new sources).
   - **Binding constraints:** a constraint that is costing the goal (see below).
3. **Do the work.** Pick 1–3 bounded items and complete them within the
   workflow's permission levels. Examples: a researched prospect list, a drafted
   post series on an untested angle, an extra run of an existing route against
   new targets, or a competitor teardown with concrete recommendations.
4. **Report.** One result in three parts: **Did for you** (with links to the
   work), **Needs you** (decisions, approvals to send or publish), and
   **Challenges** (constraints worth reconsidering). Every item names the metric
   it should move and when Pulse will check.
5. **Follow up next pass.** Did the metric move? Keep, adjust or drop the item.
   Uses existing goal metrics; no new ledger.

### Permission levels

A new `pulse.autonomy` block in `workflow.json`, set from the Pulse UI:

| Level | What Pulse may do | Default |
|---|---|---|
| Prepare | Research, analysis, drafts, lists and plans, written to `pulse/work/<date>/` | always on |
| Run | Run existing routes or steps (`execute_step`, `run_full_workflow`) extra times or on new targets, within every constraint | **auto** (user can switch to ask) |
| Outward | Anything visible outside: send, post, contact, purchase, change external records | always asks per item; fully prepared, one-click approve |
| Change the workflow | Plan, step or schedule edits | proposal with a ready-to-apply patch; applied by the existing decision-drain after approval |

The Outward level never auto-executes. Pulse still does all the preparation, so
the user's part is only the approval.

### Challenging constraints

Constraints are treated as hypotheses to test with the user, never silently
violated and never silently obeyed forever.

1. **Classify each constraint** when Goal Work first reads it:
   - **Boundary:** safety, legal, ethics, account safety, no fabrication, spend
     limits. Pulse may only ask for clarification; it never proposes loosening
     one.
   - **Choice:** theme, format, length, cadence, channel, audience, pricing,
     tone. Open to evidence-based challenge.
   - **Unknown provenance:** no "(user request, date)" marker, so the builder may
     have written it. Challenge these first: confirm them or remove them.
2. **Challenge only with evidence.** A challenge is a `pulse_issues` row with
   `issue_type='constraint_challenge'` plus a linked `pulse_decisions` row. It
   states the constraint, what it appears to cost the goal (metric, research or
   benchmark), the proposed revision, and a small reversible test.
3. **The user chooses:** keep / test the variant for N runs / change it. "Test"
   runs a bounded experiment; the constraint in `soul.md` changes only if the
   user picks "change" (applied through `/setup-goals`).
4. **Respect the answer.** "Keep" closes the issue with
   `action_taken='user kept constraint: <reason>'`. Pulse re-raises it only with
   materially new evidence, cited against the earlier answer.
5. **Never break a constraint while challenging it.** Until the user answers,
   the constraint stays binding.

Example (LinkedIn): "Theme lock" and "150–350 words" are Choices. If follower
growth is flat, Goal Work can show engagement data for on-theme versus adjacent
topics and propose one adjacent-topic test post. "Account safety" and "no
fabricated content" are Boundaries and are not up for loosening.

### Platform upkeep

- Plan Drift stays exclusive for Technical and Architecture only. Goal Work can
  still prepare and research while Drift is due; only its Run level waits.
- Technical and Architecture run on their own cadence after Goal Work and are
  collapsed in the UI under "Platform health".
- `harness_issue` rows (platform defects) go to the platform register, not the
  user's Pulse view.

## Scheduling

**Today (since 2026-08-30, `de8f24a95`):** each normal schedule has
`pulse_mode`: `off`, `basic` (backup, publish, notify) or `full` (the whole
Pulse review right after that run). The dedicated `pulse_review_only`
schedule is retired. `record_pulse_fast_request` still looks for that retired
schedule, so it can never fire. Result: several full Pulse passes per day on
busy workflows (salesoutreach 4/day, substack about 3/day, social-media about
2/day).

### Decided (user, 2026-09-23)

- **Normal schedules:** `pulse_mode` is only `off` or `basic` (backup,
  publish, notify). `full` is not valid on a normal schedule: the schedule API
  rejects it and reads legacy `full` as `basic`.
- **Pulse has its own schedule and decides when it runs next.** At the end of
  each pass, Pulse picks its next run time from when useful evidence will exist
  (for example "Tuesday's post results mature Friday, so next Pulse is Friday
  07:00") and records the reason. This reuses the Gate's existing
  `next_check_at`.
- **Limits enforced by the platform:** at most once a day, at least once a
  week. These are guards, not the schedule.
- **Runs early when:** the user answers a Needs-you decision; a normal run's
  finalizer calls `record_pulse_fast_request` (re-pointed at this schedule; it
  currently looks for the retired schedule and can never fire); or the user
  presses Run now. Early runs still respect the once-a-day limit, except Run now.
- **User override:** the Pulse tab shows "Next Pulse: Fri 07:00, because …"
  with Run now and Change. The user can pin a fixed cron instead.

```json
"pulse": {
  "enabled": true,
  "schedule": { "mode": "self", "min_interval": "24h", "max_interval": "168h", "timezone": "Asia/Kolkata" }
}
```

`"mode": "fixed"` with `"cron"` is the pinned alternative. `pulse.schedule` is
not an entry in the normal schedule list, so it never competes with producing
runs. The scheduler arms one timer per workflow at the earliest of Pulse's
chosen time, the max-interval deadline, or a pending fast request.

- **Migration:** a manual workflow contract upgrade. `full` schedules become
  `basic`. Every workflow with `pulse.enabled` gets `mode: self` with its first
  run at the next 06:00 local. Old `pulse_mode` values stay readable for
  rollback.

## UI overhaul

### What the page is today (`PulseView` → `PulseWorkspace`)

Top to bottom: On/Off header with "x/y statuses recorded"; `SoulViewer`;
`GoalProgress`; `PulseReviewOverview` (Strategy card + Strategic proposals,
Technical + Architecture cards, Drift banner, a detail panel with coverage
categories); `ReportHumanInputPanel`; "Issues and follow-through" with ten
lifecycle filters (Pulse to fix, Queued for Pulse, Waiting for evidence, …);
Platform improvements; Review run history; footer on/off toggle.

Mostly Pulse's own machinery. The ten filters and the improvements/outcome
components describe lifecycles PLAT-326 already retired.

### New layout: two tabs

**For you** (default):

1. **Goal header**: primary goal, its metric and trend, Pulse schedule
   ("next run Tue 09:00 · last run …"), **Run Pulse now**.
2. **Needs you** (shown only when non-empty): prepared work awaiting approval
   (post, send, run), constraint challenges (keep / test / change), questions.
3. **Did for you**: latest Goal Work items with links to the work in
   `pulse/work/`, the metric each targets, and its state: done → waiting to
   see effect → worked / didn't work.
4. **Next up**: gaps Pulse found but has not acted on yet.
5. **Goals & rules**: goals and constraints from `soul.md`, each constraint
   tagged Boundary / Choice / Unconfirmed, with a badge when it is being
   challenged.
6. **Settings**: Pulse on/off, next Pulse time and why (Run now / Change / pin a
   fixed schedule), permission levels.

**Platform health** (secondary tab, one status line on the For you tab
linking to it):

- Plan Drift, Technical and Architecture cards with Run automatically / Run now.
- Open / Closed issue list (two filters, not ten).
- Review run history.

### Removed

- The ten lifecycle filters (become Open / Closed).
- `PulseImprovements` and `PulseMetricOutcomes` (render the retired impact
  ledger); Goal Work results replace them.
- Coverage-category panels.
- "x/y statuses recorded" subtitle (replaced by the goal header).

### Data needed

Goal Work items are `pulse_issues` rows with `issue_type='goal_work'`:
`description` = the gap, `action_taken` = what Pulse did plus links. The
follow-up effect goes in the next Goal Work review summary and is attached to
the item. Constraint challenges are `issue_type='constraint_challenge'` with a
linked `pulse_decisions` row. No new tables.

## Implementation plan

Each phase ships on its own and is verified end to end on one pilot workflow
(LinkedIn proposed), with an agent JSON sign-off, not unit tests alone.

### Phase 0: Pulse gets its own self-deciding schedule

- `scheduler_routes.go`: reject `pulse_mode=full` on normal schedules.
- `workflow_manifest.go` `EffectivePulseMode`: normal schedules yield only
  `off` / `basic`; add `pulse.schedule` to the manifest.
- `scheduler.go`: one Pulse timer per workflow (earliest of `next_check_at`,
  max-interval deadline, pending fast request, respecting min interval). It runs
  the existing manual Pulse-only path (`PulseOnly`), which reviews the latest
  saved evidence.
- `pulse-gate.md` / finalizer: the pass ends by recording its chosen next run
  time and reason.
- `pulse_fast_requests.go` + `pulseScheduleTimingSummary`: target
  `pulse.schedule` instead of the retired `pulse_review_only`.
- Migration as above; fix the `PulseView.tsx` footer text.
- Acceptance on the pilot: the normal Engage run does only backup, publish,
  notify; the Pulse runs on its own and records a reasoned next time; a fast
  request pulls it earlier; the once-a-day and once-a-week guards hold.

### Phase 1: Goal Work does work (prompt + scope + order)

- New `guidance/templates/system/goal-work.md` replacing `strategy-auditor.md`:
  the loop above, the three gap types, and a "did / needs / challenges" result.
  Much shorter than the current prompt; no category checklist.
- `background_review_scope.go`: split `strategic_review` from
  `architecture_review`. Goal Work gets the Prepare level (writes to
  `pulse/work/`) and, gated by `pulse.autonomy.run`, `execute_step` /
  `run_full_workflow`. The `workflow_contract_execution_guard` still applies.
- `scheduler.go` + `pulsemodules.ExecutionOrder`: Goal Work runs first. The Drift
  exclusivity check (`if !planDriftDue`) no longer skips it; it only disables
  the Run level.
- `pulse-gate.md`: Goal Work is due every pass unless nothing changed and no
  work item is waiting on a checkpoint.
- Acceptance: one manual Pulse pass on the pilot produces at least one completed
  work item tied to the primary metric, with no constraint violated.

### Phase 2: Constraint challenges

- `goal-work.md`: classification and challenge rules.
- `issue_type='constraint_challenge'` on `pulse_issues`; decision options
  keep / test / change; re-raise only with new evidence.
- `/setup-goals`: records provenance on each constraint and applies a "change"
  answer to `soul.md`.
- Acceptance: on the pilot, at least one evidence-backed challenge is raised,
  "keep" is respected on the next pass, and nothing is violated in between.

### Phase 3: UI overhaul + permission settings (see "UI overhaul")

- `pulse.autonomy` in `workflow.json` with UI toggles next to the existing
  per-reviewer controls.
- The Pulse tab leads with Did for you / Needs you / Challenges and goal
  progress. Platform health moves to a collapsed section.
- The Pulse summary email and notification lead with the same three parts.

### Phase 4: Demote platform upkeep

- Technical and Architecture on a slower default cadence; harness issues routed
  to the platform register.
- Measure per workflow: Goal Work items completed, approvals given, and primary
  metric movement. Stop judging Pulse by issue counts.

## Open decisions for the user

Decided on 2026-09-23: Goal Work direction, permission levels, constraint
challenges, the two-tab UI, normal schedules off/basic only, and a
self-deciding Pulse schedule.

Answered by the user on 2026-09-23:

1. **Run level defaults to autonomous:** Pulse runs existing steps and routes on
   its own, within every constraint. Users can switch a workflow to "ask first".
2. **Scope is all workflows, not one pilot.** Each phase is still verified end to
   end on one workflow before the migration runs on the rest.
3. **Boundary constraints:** Pulse only asks for clarification. It never
   proposes loosening them.
4. **Guards:** at most daily, at least weekly; first run after migration at
   06:00 local.
