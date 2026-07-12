# Workflow self-improvement & reporting — system overview

**Start here.** This is the map of how a workflow keeps itself healthy and moving toward
its goals, and how that work is made visible and steerable for the user. It ties together
five parts that are otherwise documented separately: **Pulse**, **Auto-improve**, the
**scheduled step messages**, **notifications**, and the **Org dashboard**. Each section
links to the detailed doc.

## Why this is the critical layer — managing at scale

This is the most important subsystem in the product: it's what makes **100+ agents and
automations manageable** by a small team. Without it, every workflow needs a human watching
it — which caps you at a handful. With it:

- each workflow **self-heals** (Pulse fixes operational breakage) and **self-improves toward
  its goal** (Auto-improve), so routine operation needs no human;
- the reporting **rolls everything up** so a human manages **by exception** — the dashboard's
  triage bar surfaces only what needs attention, notifications fire only on real transitions
  (broke / recovered / new finding), and big changes wait as **proposals to approve**. You
  take in a hundred automations at a glance and act only where the system flags it.

Span of control is the whole point: the system handles the routine fixing and improving and
surfaces only the **exceptions + decisions**, so human effort scales with the number of
*exceptions*, not the number of *workflows*. That is the difference between running 5
automations and running 100+.

## 1. Purpose — two jobs

1. **Fix/improve workflows toward their goals.** Keep them *working*, and move them toward
   *winning*.
2. **Make that legible and steerable.** Report what's happening so the user can *see*, get
   *alerted*, and *decide* — because the system proposes big changes rather than applying
   them silently.

Everything below is one of those two jobs, or the substrate that connects them.

## 2. The two loops

Two loops run per workflow. They answer different questions and own different halves of a
workflow's state.

| | **Pulse** | **Auto-improve** |
|---|---|---|
| Job | **FIX** — keep it *working* | **IMPROVE** — make it *win* |
| Axis | 🩺 operational + 💵 spend/time ("does it run right, and did it spend sanely?") | 🎯 goal ("is it achieving its goal?") |
| Trigger | **after every run** (reactive) | **scheduled** (proactive) |
| Autonomy | applies low-risk fixes itself (`Pulse Bug Review/Fixer`) | **proposal-only** for big changes (replan); user/builder approve |
| Statuses | healthy / bug / critical; normal / elevated / missing cost | on-track / at-risk / off-goal |
| Code | `runPostRunMonitor` / `postRunMonitorSteps` (`scheduler.go`) | `optimizerScheduleMessages` (`scheduler.go`) |
| Guidance | `post-run-monitor.md`, `optimize-playbook.md` | Goal Advisor guidance selected by Pulse Gate |
| Detailed doc | `pulse_consolidation.md` | `auto_improvement_framework.md` |

**Pulse step sequence** (one focused turn per step): triage → fix/harden → artifact review → LLM/cost report
→ backup → publish → notify.

**Auto-improve step sequence:** pre-backup → improve → final backup → publish → notify.
The improve turn can also **adjust its own cadence** (run more often while actively
improving, back off when stable) via `update_workflow_schedule` — see
`workflow_scheduling.md`.

"Working but off-goal" is a normal, important state: Pulse says it runs fine, Auto-improve
says it isn't moving the goal yet.

## 3. The shared substrate (the loops' memory)

Both loops read and write **`builder/improve.html`** — the per-workflow Pulse/improve log,
newest-first. It *is* the loop's memory:
- **Verdict pills** (Bug, Goal), stamped with the run they're as-of.
- **Goal card** — each success criterion's Met/Short/At-risk + evidence.
- **Decision cards** — each fix (harden/replan) the loop applied.
- **Auto-improve major decision cards** — visually distinct decision entries with `Why now`,
  evidence, change, expected impact, files touched, and remaining risk/gap, so material
  replans/report/eval/cadence changes do not look like routine Pulse notes.
- **Self-verification** — on a later run the loop *confirms the last unconfirmed Decision*:
  `ok` (cite before→after), `bad` (regressed → reopen a finding), or `flat` (path not hit →
  stays pending). So "I fixed X last run → re-check X" is built in.
- **Open findings** — anchored ids that persist across runs until a fix closes them.
- **Human input requests** — structured question cards with status, options/default, and evidence; notifications may point to them, but email is not the source of truth.

See `review-improve-log.md` for the log's structure and the confirm-Decision rules.

Each loop also writes a compact **dashboard card** in the workflow's own workspace, every
run (overwrite), via current workspace write paths such as `diff_patch_workspace_file`:
- Pulse final notify/summary step → `builder/card.health.html` (🩺 final post-Pulse status + compact named fields for state/fix/evidence/next, not the full email narrative)
- Pulse report step → `builder/card.cost.html` (💵 cost/time status + headline/metric)
- Auto-improve → `builder/card.progress.html` (🎯 status + goal + headline)

These are served to the UI by `getBuilderDoc(workspace, "card-health"|"card-progress"|"card-cost")`
(`auto_improvement_endpoints.go`).

## 4. The reporting / steering surfaces

The same verdicts/Decisions/cards the loops produce while fixing are what surface here —
*#2 is #1 seen again*, never a separate analysis.

- **Notifications — `notify_user`** (active, "you need to know this"). Fans out to connected
  channels: **Gmail** (`email_html` rich body + `email_body` plain fallback — must be
  **inline-styled**, Gmail strips `<style>`/`<head>`), **WhatsApp**, **Slack**. Deliberately
  **sparing** — only on a decision-worthy transition (broke / recovered / new finding);
  silence on a steady run. Code: `virtual-tools/human_tools.go`, `services/gmail_service.go`.
- **Org dashboard** (passive, "where things stand right now"). The default Org view. It
  enumerates workflows, reads each one's two cards, parses the `data-*` attributes, and
  renders its own React components: a **triage bar** (N need attention + health/goal counts),
  **cards grouped by goal** (🩺+🎯 pills, headlines, "updated Xm ago"), plus loading /
  no-automations / warming-up / error states. Code: `frontend/.../org/OrgDashboard.tsx`,
  `EmployeeDashboard.tsx`. Detailed doc: `org_dashboard_design.md`.
- **Org Pulse** (the org-level daily pass — Chief of Staff). Maintains `pulse/goals.html`
  (the goal scorecard) and `pulse/org-pulse.html` (the dated journal), harvests cross-workflow
  insight into CoS memory, and writes **proposal-only** org recommendations. Code:
  `builtin_schedules.go`. Guidance: `org-pulse.md`, `org-html.md`, `org-goals.md`.

**Oversight is part of reporting:** because big changes are proposal-only, these surfaces are
also where the user *decides* (approve a replan, act on a recommendation), not just a window.

## 5. How it fits together

- **One substrate, two uses:** the loops' verdicts/Decisions/cards drive *both* the fixing
  (harden/replan) and the reporting (improve.html → dashboard → notifications).
- **Cadence:** Pulse runs per-run; Auto-improve runs on a schedule it can self-adjust;
  Org Pulse runs daily. So the dashboard is fresh per-run (cards), while goal judgment is the
  CoS's daily call.
- **Ownership (no write contention):** each workflow owns its `improve.html` + cards in its
  own workspace; the CoS owns `pulse/goals.html` + `pulse/org-pulse.html`. The dashboard
  *assembles* at view time — nothing shared is written by two writers.

## 6. Code + docs map

| Concern | Code | Doc |
|---|---|---|
| Pulse loop | `scheduler.go` (`runPostRunMonitor`, `postRunMonitorSteps`) | `pulse_consolidation.md`, guidance `post-run-monitor.md` |
| Auto-improve loop | `scheduler.go` (`optimizerScheduleMessages`, `wrapOptimizerImproveMessage`) | `auto_improvement_framework.md`, guidance `optimize-playbook.md` |
| Self-schedule cadence | `workflow_schedule_tools.go` (`update_workflow_schedule`) | `workflow_scheduling.md` |
| improve.html log | served via `auto_improvement_endpoints.go` | guidance `review-improve-log.md` |
| Dashboard cards | card writes in loop prompts (`scheduler.go`); `getBuilderDoc` card kinds (`auto_improvement_endpoints.go`) | `org_dashboard_design.md` |
| Notifications | `virtual-tools/human_tools.go` (`notify_user`), `services/gmail_service.go` | (this doc) |
| Org dashboard UI | `frontend/.../org/OrgDashboard.tsx`, `EmployeeDashboard.tsx`, `OrgHtmlPanels.tsx` | `org_dashboard_design.md` |
| Org Pulse (CoS) | `builtin_schedules.go` | guidance `org-pulse.md`, `org-html.md` |
| Monitoring overview | — | `workflow_monitoring.md` |

## Open / evolving
- Dashboard cards are **v1: current status** (rolling trend deferred).
- Pulse self-verification confirms a Decision **once** when the path is next exercised; a
  persistent "watch for N runs" list would re-verify over several runs.
- Auto-improve **can** change its cadence and the hook exists, but the *policy* (exactly when
  to speed up vs slow down) is light and could be tightened.
