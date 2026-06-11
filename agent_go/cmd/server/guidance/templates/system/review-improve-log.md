## Workflow log conventions

Canonical conventions shared by every `/review-*` and `/improve-*` skill, the post-run monitor, and any chat-driven fix that touches the log. Load this once; the individual skills point here instead of restating it.

### One log: `builder/improve.html`

The workflow keeps a **single durable log** — `builder/improve.html` — the workflow's journal and the user's primary window into it. Everything the user should be able to see later goes here, in one place:

- **applied or proposed changes** (what `/improve-*`, harden/replan, and chat fixes did, and why),
- **review findings** (what `/review-*` flagged — recommendations, REVIEW = recommend, do NOT apply),
- **run notes and the recent-run log** (what happened on recent runs),
- **monitor observations** (post-run regressions / drift the monitor caught),
- **user rules** (authoritative constraints the user stated).

`builder/review.html` is **legacy**. If you encounter one with unresolved findings, fold them into `builder/improve.html` as open-finding entries, then stop writing to it. Do not create new `review.html` files.

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline. The target layout is the single-log view: a header with the workflow's current health, **signal tiles** (metrics as numbers), a compact **recent-runs** strip, a **newest-first timeline** of entries, and an **archive index**. Match that structure and that level of polish.

### Newest on top — always

New entries go at the **top** of the timeline, not appended at the bottom. The file carries a stable anchor comment `<!-- LOG ENTRIES: newest first -->` directly below the header/tiles; insert each new entry immediately after it with `diff_patch_workspace_file`. Never reorder or rewrite existing entries except to close out an open finding (below). **Always read the existing file first** so you continue its style and don't duplicate entries.

### Entry kinds

Each entry is a small card: a date, a kind tag, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below).
- **User rule** — a constraint the user stated. Mark it clearly as authoritative ("USER RULE — authoritative") so future agents treat it as a hard constraint, never silently override it. This replaces the old `source: "user"` field — say it in words.
- **Note** — a freeform observation or watchpoint that explains weird runs ("staging UI is mid-redesign, expect selector churn through ~June 20 — not a workflow bug").
- **Open finding** — something wrong that is not yet fixed. Give it a short stable anchor id (e.g. `id="of-2026-06-07-screenshots"`) **only so a later Decision can mark it resolved** — that is the one place an id earns its keep. No other entry kind needs an id.

### Closing out an open finding

When a change fixes an open finding, edit that finding's card in place to add a resolved line — don't delete it, don't open a duplicate:

```
Resolved 2026-06-09 — added a non-empty-screenshot pre-validation rule to audit-finalizer.
```

Reference the finding by its anchor id (or, if it has none, by its date + title). This keeps the "what's still outstanding vs. what's been handled" view honest.

### Metrics as prose, never charts

Render current metric values as readable tiles or lines — the value plus its movement in words (`eval 0.86 ▲ from 0.79`, `cost ¢19 ▲ from ¢12`). Read them from `planning/metrics.json`, `db/metrics_history.jsonl`, and `scores/evaluation/`. These files are the deterministic source of truth — quote them, never fabricate a number or a trend.

### Keep the active file small

The log must not grow without bound. When `builder/improve.html` passes roughly **800 lines, 60 KB, or 20 timeline entries**, move older **resolved** findings, superseded decisions, and routine run rows into a monthly archive `builder/improve-archive/YYYY-MM.html`, leaving a one-row entry in the Archive Index (date range, count, any still-unresolved ids). **Never archive** open findings, user rules, current notes, or the latest few entries — the active file should always answer "what's the state of this workflow right now and what still needs attention."

### Legacy `.md` / structured-block migration (one-time)

Some workspaces still have legacy `builder/improve.md` / `builder/review.md`, or an `improve.html` full of old ```improve-decision``` fenced JSON blocks and `F-…`/`I-…` ids. When you open such a file: read it in full, carry every unresolved finding and still-relevant decision forward as readable timeline entries in the new format, then delete the `.md` (via `execute_shell_command`) or replace the JSON blocks with prose cards. Do this once, before writing new entries, so nothing is lost. The structured JSON schema and the dual `F-/I-` id system are retired — write readable prose instead.
