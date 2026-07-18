# Pulse: post-run pipeline consolidation

> **Status (2026-07): superseded and evolved.** The June-2026 consolidation below
> shipped, but Pulse has since moved past the fixed "back up → triage → fix →
> notify" pipeline described in **Target model**. Read **Current architecture**
> first; the sections after it are kept as the historical migration record. The
> canonical, always-current spec is the `post-run-monitor` reference doc
> (`agent_go/cmd/server/guidance/templates/system/post-run-monitor.md`); this
> file only explains the shape and why it got here.

## Current architecture (2026-07)

Pulse is a **dynamic post-run steward**, not a fixed checklist. After each
scheduled run it runs a small sequence with one mandatory intelligence turn:

1. **Gate / Worklist.** One turn reads the run evidence (run summary, `CONCERNS:`
   markers, changelog, eval/report/DB/KB/learnings state, human inputs, Chief of
   Staff recommendations, cost/tier signals, and the store freshness ledgers) and
   calls `record_pulse_worklist` exactly once with one `due|skipped` decision for
   each of the ten modules (`bug_review`, `artifact_review`, `report_health`,
   `eval_health`, `learning_health`, `knowledgebase_health`, `db_health`,
   `cost_llm_time`, `llm_ops_review`, `goal_advisor`). Every skip carries a
   next-check condition. Go enforces the complete-worklist rule so a module can't
   silently disappear. Gate mutates nothing.
2. **Parallel read-only reviewers.** The scheduler dispatches only the `due`
   modules. Each is reviewed by an independent `call_generic_agent` reviewer
   (batches of ≤4) that **only inspects and advises** — read-only tool allowlist,
   empty write paths, no `builder/improve.html` writes. Each reviewer loads its
   own deep brief on demand (`pulse-bug-review`, `improve-*`, etc.), which keeps
   the frequent Gate turn lean.
3. **Single Pulse Fixer.** The parent turn is the **only writer**. It consolidates
   findings, resolves conflicts by a fixed precedence, applies bounded safe fixes
   sequentially, and verifies each against the single `fix-verification` contract
   (a successful write is never proof; a fix stays `changed_unverified` until a
   real run/eval/report confirms it). Strategy/LLM changes remain proposal-only
   behind the human-input approval flow.
4. **One ordered finalizer.** dashboard → backup → publish → notify, each recording
   its own live/final status. The scheduler marks anything left running as failed.

Goal Advisor is a Pulse-selected module (not a separate schedule). Recovery,
timeout, and concurrency are hardened in Go: a trusted-session registry binds each
logical Pulse run (recovery sessions included) so only the owning session can write
its module/worklist/final-command state, agent writes are compare-and-set against
the recorded run and never overwrite a scheduler-recorded terminal state, and a
boot sweep reconciles final commands stranded by a crash.

**Store freshness (2026-07).** A code-owned ledger (`learnings/_global/_freshness.json`,
`knowledgebase/_freshness.json`) records when each store — and each reference
file / topic note — was last confirmed by a run, stamped by the runtime at the
learnings/KB contribution turns (not LLM-maintained, so it can't desync). Gate
marks `learning_health` / `knowledgebase_health` due on a confirmation-recency
signal, and the reviewer re-verifies → refreshes / demotes / retires aging
knowledge (never deletes on age alone). This adds a *time/decay* axis to what was
previously only contradiction-driven staleness.

---

## Historical migration record (2026-06)

Status at the time: **Implemented** (2026-06-21) — Phase 1 (rename) + Phase 2 (backup-always +
Pulse-does-low-risk-fixes). Auto-fix was intentionally scoped to low-risk reversible
harden; bigger `replan` changes stayed with the scheduled auto-improve loop. (Both
the fixed 4-step pipeline and the separate auto-improve loop below were later
replaced by the dynamic Gate/worklist model in **Current architecture**.)

## Pulse vs the auto-improve loop (division of labor)

These are two tiers over the same Pulse log / Bug-Goal vocabulary — they compose,
they don't overlap:

- **Pulse** — runs after **every** run (when enabled). Cheap + immediate: **back up →
  triage → apply a low-risk reversible Bug harden → record a Goal replan *proposal* →
  notify.** Never runs `replan` or a full improvement pass itself.
- **Auto-improve loop** — a **separate schedule** (`/auto-improve`, optimizer mode).
  Owns the **bigger changes**: batched harden and the **`replan`** tool for structural
  plan rewrites, acting on the proposals Pulse recorded. Pulse is skipped after these
  optimizer-mode runs (`scheduler.go`: `WorkshopMode != "optimizer"`), so the two never
  fight over the same run.

## Problem

After a run there are **four disconnected mechanisms**, with different triggers,
gating, and reliability:

1. **Post-run monitor** (`runPostRunMonitor`, `scheduler.go:1164`) — opt-in
   (`post_run_monitor`), a dedicated agent pass that writes the Pulse log
   (`builder/improve.html` time-stamped Signal / Reflection / Improvement history; Goal / Ikigai comes from `soul/soul.md`). Its final notify/summary step
   writes the `builder/card.health.html` dashboard card after harden, artifact review,
   cost/time, backup, and publish are known. Auto-improve **cadence #1**.
2. **Scheduled harden** — auto-improve **cadence #2**, applies low-risk Bug fixes on
   its own schedule.
3. **Scheduled replan-proposal** — auto-improve **cadence #3**, proposes Goal changes.
4. **Backup** (`workflowRunBackupDirective`, `background_agents.go:1607`) — a directive
   appended to the builder's AUTO-NOTIFICATION after a `run_full_workflow` completion.
   Best-effort steering, **not** a guaranteed step; scheduled runs don't back up
   themselves (`scheduler.go:1268`).

Naming reality: the monitor reference doc already calls `builder/improve.html`
"**the Pulse log**". So "monitor" (the pass) and "Pulse" (the log) are two halves of
one feature that was never unified in the UI.

## Decisions (user, 2026-06-21)

1. **Pulse everywhere.** The feature/toggle is named **Pulse**. The right-panel tab is
   **Pulse** (reverts the Phase-3 "History" rename). Internally the monitor pass becomes
   the "Pulse pass."
2. **Full auto.** Enabling **Pulse** runs the complete post-run loop **every run**:
   **back up → triage → apply fixes (harden for Bug, replan for Goal)**. The four
   mechanisms above collapse into this one toggle.

## Target model

```
Pulse (one toggle) → after every run:
  1. Back up        — always, guaranteed (local-git default = zero-config). Skipped only
                      when source_hash is unchanged (no empty commits).
  2. Triage         — the current monitor: Bug + Goal verdicts, Pulse log, verdict signal.
  3. Fix            — Bug → harden (low-risk reliability/contract fixes);
                      Goal → replan (now applied, was propose-only).
  4. Notify         — one transition notification (unchanged policy).
Pulse tab (UI) → the durable record: Timeline (improve.html) + Plan edits (changelog).
```

Backup is no longer a separate steering directive; it's step 1 of the Pulse pass and
runs in the scheduler post-run block where the monitor already lives.

## Changes required

### UI (rename — safe, mechanical)
- `WorkflowCanvas.tsx`: tab label + titles "History" → **"Pulse"** (revert Phase 3).
- `HistoryView.tsx` → `PulseView.tsx` (component rename; keeps Timeline + Plan edits
  sub-tabs). `PlanChangelogFeed.tsx` unchanged.
- `WorkflowToolbar.tsx`: the monitor toggle (`monitorOn`) relabels to **"Pulse"**.

### Behavior (the real change — needs care)
- `runPostRunMonitor` → `runPulse` (or keep name, expand prompt). Add **backup as step 1**
  (guaranteed, source-hash gated) and **fix as step 3** (harden + replan).
- Rewrite `guidance/templates/system/post-run-monitor.md`: it is no longer read-only —
  it backs up, then triages, then applies the safe fixes. Reconcile with the existing
  harden / replan reference docs so there's one fix contract, not three.
- `scheduler.go`: backup runs here for scheduled runs (always). Remove / demote the
  `workflowRunBackupDirective` steering path so backup isn't double-driven.
- `PostRunMonitor` manifest flag stays the gate but is surfaced as "Pulse enabled".

### Risk / open
- **Auto-fix every run changes the safety model.** Cadences #1–#3 were deliberately
  separated (cheap read-only triage vs riskier fixes on a slower schedule). Folding them
  means a fix can land on every run. Mitigation: keep harden's "low-risk only" contract;
  keep replan as the heavier action and decide whether it truly applies or still proposes
  for high-risk plan rewrites. **To confirm before the behavioral rewrite ships.**
- Cost: every run now does backup + triage + (sometimes) fix instead of a cheap triage.
  Acceptable per the "full auto" decision, but worth a source-hash / no-op fast path.

## Backup visibility (2026-06-21)
Backup surfaces in three places now: the toolbar status dot + the Backup popup
(existing), and — added here — the **Pulse log Run row**. Since Pulse owns the backup,
its step-3 Run row records the backup result (`backed up ✓ <commit>` /
`unchanged — already backed up` / `backup ✗ <reason>`). Doc-only change to
`post-run-monitor.md` (step 3) and `review-improve-log.md` (Run kind) — agent-driven,
no Go.

## Phasing
1. **Rename to Pulse** ✅ Done (2026-06-21, UI only). `WorkflowCanvas` tab + titles
   "History" → "Pulse"; `HistoryView.tsx` → `PulseView.tsx`; toolbar "Monitor" button +
   help popup → "Pulse" (internal vars `monitorOn`/`post_run_monitor` unchanged).
   Verified: tsc 0, lints clean.
2. **Backup always + Pulse does low-risk fixes** ✅ Done (2026-06-21). Rewrote the
   Pulse pass prompt in `scheduler.go` (`runPostRunMonitor`) to the 4-step contract
   (back up → triage → low-risk harden / replan-proposal → notify) and rewrote
   `guidance/templates/system/post-run-monitor.md` to match (new "0. Back up first" +
   "3b. Apply the fix" sections; dropped the strict read-only framing; kept step 5 =
   Notify so the prompt's reference still resolves). Backend build + vet OK.
   Safety rails kept: **low-risk reversible fixes only** (bigger work → auto-improve
   loop), and **source-hash gate** so steady runs skip the push.

3. **Unify the backup directive** ✅ Done (2026-06-21). `workflowRunBackupDirective`
   (the interactive arm) now shares **one backup contract** with Pulse's step 1: same
   zero-config local-git default, same source-hash skip. So the two arms (Pulse =
   scheduled, directive = interactive + Pulse-off fallback) can't double-push — whichever
   runs second sees the source already backed up and skips. Stale `scheduler.go:1268`
   comment corrected. Build + vet OK; no test pinned the old wording.

   **Non-goal (decided 2026-06-21):** do NOT move the source-hash skip into deterministic
   Go code. The whole post-run/backup loop stays agent-driven — the agent reads
   `backup/status.json` and decides. No Go-side gating coupling.

## Phase 4 — Publish folded into the Pulse loop (2026-06-24)

Publish (the public-URL twin of Backup — see `docs/workflow/publish_design.md`) is now a
step of the Pulse pass, so a workflow's public dashboard stays current automatically:

```
Pulse → after every run:
  1. Back up    — guaranteed, source-hash gated (unchanged).
  2. Triage     — Bug + Goal verdicts, Pulse log (unchanged).
  3. Fix        — low-risk harden / replan proposal (unchanged).
  4. Re-publish — ONLY if publish is configured + enabled; rebuilds from source and
                  redeploys both artifacts. Skipped when publish is off.
  5. Notify     — one transition notification (unchanged).
```

`post-run-monitor.md` gained a "### 4b. Re-publish (only if publish is on)" section. Like
backup, publish is **agent-driven and read-only in the UI**: setup/run/restore/publish all
happen in the builder chat via the **`/backup`** and **`/publish`** slash commands; the
toolbar popups are status-only. The dead write endpoints (`/workflow/{backup,publish}/{config,run}`)
were removed.

### Publish output contract (what `/publish` ships)
- **Both artifacts, always** — the baked report **dashboard** (`dashboard.html`) AND the
  **Pulse log** (`pulse.html`, from `builder/improve.html`), joined by a top-nav
  `index.html` wrapper (Dashboard | Pulse tabs). Publishing only one is a bug.
- **Dark only, matching the app** — the published pages must set **both** theming hooks on
  `<html>`: `class="dark"` (the app's Tailwind mechanism, `ThemeContext`) **and**
  `data-theme="dark"` (what report widgets `HtmlWidgetFrame` and the Pulse-log skeleton
  key on). Setting only `data-theme` left the dashboard light, because report widgets key
  primarily off the `.dark` class. No toggle, no `prefers-color-scheme` (that follows the
  viewer's OS). See `[[project_published_page_theme_contract]]`.
- **Stage outside the workspace** — deploy CLIs (`netlify`, `vercel`, …) write state
  (`.netlify/`, `.vercel/`) to their CWD. The workflow folder is writable EXCEPT
  `planning/`, but the CLI's CWD is often the docs root (above the folder, outside the
  write allow-list), so it gets rejected. Copy the finished static files to
  `/tmp/publish-<workflow>/` and run the deploy from there.

### Plan-edits consolidation (2026-06-24)
The toolbar **Plan edits** popup (the granular `planning/changelog/*.json` audit feed) got a
**Consolidate** control — drop edits older than 7/30/90/180 days — backed by
`POST /workflow/plan-changelog/prune`. The server prunes (deletes whole `changelog-*.json`
files older than the cutoff) because `planning/` is shell-guarded from the agent.
