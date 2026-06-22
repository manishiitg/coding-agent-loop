# Org Pulse — the Chief of Staff's daily heartbeat

Status: **Design** (2026-06-21). Not implemented.

## The model this sits in

The **Chief of Staff (CoS)** is the multi-agent chat — a single agent identity that
runs the org. It stands on **two primitives** (2026-06-22 — collapsed from four):

| Primitive | What it is |
|---|---|
| **Memory** | what the system *knows* — durable, entity-based (`entities/*.md`), auto-enriched. Includes procedural "how we do X" knowledge applied inline. |
| **Workflow** | what the system reusably *does* — any repeatable task, one step or twenty (own plan / eval / Pulse / backup). |

Everything else dissolves into these two:
- **Ad-hoc task** — not a stored thing; it's just chat. If it recurs it *becomes a
  workflow*. Transient activity, not a primitive.
- **Skill** — deleted as a separate tier. A *runnable* reusable procedure is just a small
  **workflow**; *how-to knowledge* the agent applies inline is just **memory**.
- **Employee** — already dissolved into memory (entities).
- **Chief of Staff** — the operator over the two axes, not a stored thing.

### The promotion ladder (now one rung)
Collapsing "skill" deletes the painful skill↔workflow boundary. The ladder is just:
**recurring ad-hoc task → workflow.** A workflow can be one step, so promoting a small
reusable task isn't heavy.

- `create_workflow` — **exists** (server.go:4419, privileged; confirmed callable from the
  CoS chat). The promotion rung is already there.
- ~~`save_skill`~~ — **dropped (2026-06-22)**. No skill tier, so nothing to build here.
- A unified **ledger** of what the CoS did (ad-hoc + scheduled) — still useful as the
  substrate for noticing recurrence; not yet built.

**Caveat:** "reusable task = workflow" only holds while `create_workflow` stays
lightweight (a minimal manifest + one-step plan). Keep that true.

## Org Pulse — what it is

A **daily, opt-in pass on the CoS chat** — the chat-level parallel to the per-workflow
Pulse toggle (`post_run_monitor`). When **on**, once a day the CoS:

1. **Judges the endgame** — reads each workflow's `builder/monitor-verdict.json`
   (`{bug, goal, headline}`, written by the per-workflow Pulse) and rolls the Goal
   verdicts into an org-level "are we achieving our goals" view. Cheap — the per-workflow
   Pulse already did the judging.
2. **Harvests** — reads each workflow's `reports/`, `knowledgebase/`, and
   `learnings/_global/SKILL.md`, **plus the stored conversation files**, and curates the
   worth-keeping bits into its own shared memory (entities/topics).
3. **Suggests** — surfaces decisions for the user, including **promotion proposals**
   ("you've done X 3× — make it a workflow?"). The ladder is driven by these suggestions.

The per-workflow Pulse is the **foundation**: Org Pulse aggregates its verdicts and
harvests its learnings. Doing per-workflow Pulse first was the right order.

## Keep it agentic, NOT an import

This is the load-bearing constraint (see the agentic-not-deterministic principle).

- **Wrong (import):** a Go job that parses `monitor-verdict.json` + learnings on a
  schedule and copies fields into a CoS KB table. Fixed schema, 1:1 mirror, rots.
- **Right (harvest):** Org Pulse is a **CoS reasoning session driven by a reference
  doc**. Go supplies only **access** (read tools it already has), **cadence** (the daily
  schedule), and **the contract** (the doc). The agent **judges** relevance, **synthesizes
  across** workflows, and **curates** into its own knowledge (merge/update, not append).
- Precedent to copy: the workflows' own agentic curation — the KB-update agent,
  `consolidate_knowledgebase`, `organize_global_learnings`. Org Pulse is the same pattern,
  one level up.
- **Memory store choice:** free-form agent-curated memory (`entities/`/topics), NOT a
  structured DB table — a rigid schema drags it back toward import. A structured store
  only earns its place later if the org knowledge needs programmatic querying, and even
  then the agent writes it (like the workflows' `graph.json`).

The rule: every time you're tempted to add a Go step that parses a workflow file and
writes a CoS file, that's an import — make it a line in the reference doc instead.

## Why the build is small (most plumbing exists)

- **Schedule mechanism:** the CoS already runs builtin scheduled passes
  (`builtin-auto-enrich-memory`, every 3h — `builtin_schedules.go`). Org Pulse is the same
  shape: a builtin **daily** schedule, gated by a toggle.
- **Toggle pattern:** workflow Pulse is the `post_run_monitor` flag flipping a scheduled
  pass on. Org Pulse is a flag in the CoS config (`multiagent-config.json`), surfaced as
  an on/off in the chat header.
- **Conversations are files:** chat history is persisted (`chat_history_persistence`), so
  the pass can read past conversations as evidence.
- **Access:** the CoS already has read-only filesystem access to every `Workflow/<name>/`
  via `execute_shell_command`.

## Build pieces

1. **Toggle + cadence** — an Org Pulse on/off in the CoS chat header. It's a schedule, so
   it carries a `cron_expression`: **default once a day, user-editable** (change the time or
   frequency like any other schedule). Stored alongside the other multi-agent schedules.
   *(Frontend chat-header toggle still pending; enable/disable already works via the
   Scheduled Tasks popup since the builtin is registered.)*
2. **Daily builtin schedule** — ✅ Built (2026-06-21). `builtin-org-pulse` added to
   `DefaultBuiltinSchedules()` (`builtin_schedules.go`): **`Enabled:false` (opt-in)**, cron
   `0 8 * * *` (daily, editable), mode multi-agent, query loads `get_reference_doc(kind="org-pulse")`.
   Turning it on = a same-ID user override with `enabled:true` + chosen cron (via
   `MergeBuiltinSchedules`). **No Go pre-fire check** — Org Pulse self-gates agentically
   (wakes daily, cheap "anything new?" check, exits if not), unlike enrich-memory's Go gate.
   Idle-day behavior: a clean no-op (writes nothing, stops). Build + tests pass.
3. **The Org Pulse reference doc** — ✅ Drafted (2026-06-21):
   `guidance/templates/system/org-pulse.md`, registered as `get_reference_doc(kind="org-pulse")`
   (multi-agent mode) in `guidance.go`. 5-step agentic contract: gather → judge endgame →
   harvest (curate/merge, never import) → spot promotions → record everything in the
   single **`pulse/org-pulse.html`** log (HTML-only, no JSON — decided 2026-06-22), notify
   only when decision-worthy. Build + guidance tests pass.
4. **Extend the CoS access recipe** — ✅ Done (2026-06-22). `employee-management.md` sweep
   now reads each workflow's `monitor-verdict.json` (health), `knowledgebase/`, and
   `learnings/_global/SKILL.md`, not just runs/db/reports.
5. **Org Pulse view** — ✅ Done (2026-06-22). HTML-only: an **"Org Pulse" button** in the
   CoS chat header (`ChatTabs.tsx`) opens `pulse/org-pulse.html` in the existing right-side
   file viewer (renders via `HtmlRenderer`), the multi-agent parallel of a workflow's Pulse
   tab. No JSON inbox, no accept/dismiss — suggestions are entries in the HTML.
6. *(Ladder, later — optional)* a unified **ledger** of CoS activity as the recurrence
   substrate. `save_skill` is **dropped** — promotion goes straight to `create_workflow`
   (which already works from chat), so there's no skill rung to build.

## Decided
- **Cadence (2026-06-21):** default **once a day, user-configurable** — it's a normal
  multi-agent schedule with an editable `cron_expression`, not a fixed time.
- **Notify (drafted into org-pulse.md):** silent on a steady day; one push only on a
  decision-worthy change (mirrors workflow Pulse's transition discipline).
- **Surface (2026-06-22):** **HTML-only**, shown on the right like a workflow's Pulse —
  `pulse/org-pulse.html` opened via the CoS chat-header "Org Pulse" button. No
  `suggestions.json`, no accept/dismiss inbox; suggestions are cards inside the HTML.

## Chat-header control (✅ Done 2026-06-22)
`OrgPulseControl.tsx` in the CoS chat header (`ChatTabs.tsx`) — mirrors the workflow Pulse
toggle. An **"Org Pulse · ON/OFF"** button opens a popup with: an explanation, an
**enable/disable switch** (flips the `builtin-org-pulse` schedule via
`schedulerApi.enableJob/disableJob`, with next/last-run status), and **"Open today's log"**
(opens `pulse/org-pulse.html` on the right). **Empty state** handled here: before the first
run it shows "No Org Pulse log yet" + a **Run now** button (`triggerJob`) to generate it on
demand — so we never dump the user into an empty file viewer.

## Open questions
- *(none — surface, cadence, notify, the ladder, the toggle, and the empty state are all
  settled/built.)*
