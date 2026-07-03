## Org HTML Design Contract

Use this before writing or materially changing:

- `pulse/goals.html`
- `pulse/org-pulse.html`

These files are product surfaces inside the Chief of Staff right panel. They are not raw
logs, markdown replacements, or decorative pages. A CEO should understand the state of the
org in the first screen: what is on track, what is drifting, what is unknown, and what
decision is needed.

Also load `get_reference_doc(kind="html-output")` for the generic HTML rules. This doc is
the org-specific structure and visual system.

### Non-negotiables

- **Self-contained HTML.** Inline CSS and JS. No CDN, no remote fonts, no external images
  unless the user explicitly supplied the asset.
- **Theme-aware.** The app injects `data-theme="light|dark"` and `class="dark"` on the
  iframe `<html>`. Define CSS variables in `:root` and override them with
  `html[data-theme="dark"], html.dark`. Keep a `prefers-color-scheme` fallback only as a
  fallback, not the primary theme path.
- **Mobile/right-panel first, publish-ready second.** It must read well in the 360-480px
  right panel by default and still look polished when published full width. The base CSS
  must be the mobile layout: stacked headers, compact two-column KPI tiles, one-column
  content cards, wrapped meta rows, and no overlapping timestamps. Use a one-column KPI
  fallback only for very narrow widths around 360px. Add tablet/desktop enhancements with
  `@media (min-width: ...)`.
  Do not design desktop first and then patch mobile with max-width overrides. No
  `height: 100vh`, no fixed page width, no inner body scroll container.
- **Operational density.** This is not a landing page. Use compact cards, tables, chips, and
  timelines. Avoid giant hero sections, decorative gradients, big illustrations, and empty
  marketing copy.
- **Evidence first.** Every status needs a source: workflow name, run number/date, report,
  db table, Pulse headline, or explicit "missing evidence". Never color a card green/yellow/red
  without the rule or evidence line that explains it.
- **Semantic structure.** Use stable ids/classes/data attributes so future agents can parse
  and update the page without guessing: `data-goal-id`, `data-status`, `data-workflow`,
  `data-date`, and the anchor comments shown below.
- **No operational state in content HTML.** Keep backup/publish config and status in the
  workflow-style org JSON files (`pulse/backup.json`, `pulse/backup/status.json`,
  `pulse/publish.json`, `pulse/publish/status.json`). Goals/Pulse HTML should stay focused
  on the user-facing scorecard and log.
- **Newest-first history.** For logs/history, prepend entries after the anchor comment. Do
  not append at the bottom.
- **No text collisions.** Long workflow names, goal titles, timestamps, table cells, and
  evidence strings must wrap or horizontally scroll inside their container. Use
  `overflow-wrap:anywhere`, `min-width:0`, stacked `.row` layouts on mobile, and reserve
  desktop-only side-by-side metadata for `@media (min-width: 640px)`.
- **Valid CSS, no escaped braces.** Write raw CSS exactly as browsers expect it:
  `@media (min-width:640px){ ... }`, never `{{"{{"}} ... {{"}}"}}`. Repeated visual elements must use
  classes from the baseline (`.kpi`, `.pill`, `.entry`, `.suggestions`), not repeated inline
  layout styles.

### Shared visual language

Both org pages should use the same shell:

- eyebrow: `chief of staff`
- title: `Org Goals` or `Org Pulse`
- meta row: last updated date/time and evidence freshness
- status banner: the one-sentence read
- KPI strip: compact tiles for counts/status
- content cards: status, evidence, next action
- timeline/history: newest first

When editing an existing `pulse/goals.html` or `pulse/org-pulse.html`, upgrade the shell if
it predates this mobile-first contract. Treat the page as needing a skeleton refresh if it
lacks `<meta name="viewport">`, uses desktop-first grid/flex rows as the base layout, has
side-by-side timestamps that can overlap text, lacks `overflow-wrap:anywhere`, or relies on
`@media (max-width: ...)` patches for the right-panel layout. Preserve the scorecard/pulse
entries and history, but rewrite the CSS/shell to the current baseline.

Status vocabulary:

| Status | Use for | Class |
|--------|---------|-------|
| `on-track` | goal/workflow is meeting the stated threshold | `.ok` |
| `at-risk` | drifting, stale, or near threshold | `.warn` |
| `off-track` | failing target or broken dependency | `.bad` |
| `unknown` | missing or insufficient evidence | `.unknown` |
| `supporting` | useful maintenance work, not directly tied to a goal metric | `.supporting` |
| `unaligned` | recurring workflow has no goal/supporting rationale | `.bad` |

### Shared CSS baseline

Start from these tokens and components. You may add local classes, but keep this foundation
so `goals.html` and `org-pulse.html` feel like one product.

```html
<style>
  :root{
    color-scheme:light;
    --bg:#f7f7f4;--surface:#fff;--surface-2:#fbfaf7;
    --ink:#191917;--ink-2:#595852;--ink-3:#8a887f;
    --line:#ebe8df;--line-2:#ded9cf;
    --ok:#3f7a4a;--ok-bg:#eaf3ea;
    --warn:#9a6a05;--warn-bg:#fbf1d8;
    --bad:#b23a2f;--bad-bg:#fbe8e4;
    --unknown:#696a73;--unknown-bg:#eeedf0;
    --accent:#8a5a44;--accent-bg:#f4ece6;
    --shadow:0 1px 2px rgba(20,20,18,.04),0 8px 24px -16px rgba(20,20,18,.18);
    --sans:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
    --mono:"SF Mono",ui-monospace,Menlo,Consolas,monospace;
    --r:8px;
  }
  html[data-theme="dark"],html.dark{
    color-scheme:dark;
    --bg:#0f0f12;--surface:#17171c;--surface-2:#121217;
    --ink:#f1f0f4;--ink-2:#aaa8b1;--ink-3:#74727d;
    --line:#292832;--line-2:#373541;
    --ok:#62d58b;--ok-bg:#10281a;
    --warn:#e8b85a;--warn-bg:#2a210d;
    --bad:#f08078;--bad-bg:#311815;
    --unknown:#aaa8b1;--unknown-bg:#24232b;
    --accent:#d8a184;--accent-bg:#261c18;
    --shadow:0 1px 0 rgba(255,255,255,.04) inset,0 10px 30px -18px rgba(0,0,0,.8);
  }
  *{box-sizing:border-box}
  html,body{margin:0;min-height:100%;width:100%;max-width:100%;overflow-x:hidden;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:14px;line-height:1.5;font-variant-numeric:tabular-nums}
  body{padding:0}
  body,.card,.entry,.kpi,.status,td,th{overflow-wrap:anywhere}
  .wrap{width:100%;max-width:980px;margin:0 auto;padding:12px 10px 48px}
  .top{display:block}
  .eyebrow{font:700 10px/1 var(--mono);letter-spacing:.13em;text-transform:uppercase;color:var(--ink-3)}
  h1{margin:6px 0 0;font-size:22px;line-height:1.08;letter-spacing:-.01em}
  .meta{margin-top:8px;color:var(--ink-3);font:520 11px/1.45 var(--mono)}
  .status{display:flex;gap:10px;align-items:flex-start;flex-wrap:wrap;margin:14px 0 12px;padding:11px 12px;border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);box-shadow:var(--shadow)}
  .status .dot{flex:none;width:9px;height:9px;border-radius:50%;margin-top:7px;background:currentColor;box-shadow:0 0 0 4px color-mix(in srgb,currentColor 16%,transparent)}
  .status.ok{color:var(--ok)}.status.warn{color:var(--warn)}.status.bad{color:var(--bad)}.status.unknown{color:var(--unknown)}
  .status .text{color:var(--ink);font-weight:620;min-width:0;flex:1 1 220px}.status .sub{margin-top:3px;color:var(--ink-2);font-size:12px}
  .kpis{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:12px 0 18px}
  .kpi{min-width:0;border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);padding:10px 11px;box-shadow:var(--shadow)}
  .kpi .label{font:700 9.5px/1.2 var(--mono);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-3)}
  .kpi .value{margin-top:6px;font-size:22px;font-weight:720;letter-spacing:-.02em}
  .kpi .note{margin-top:3px;color:var(--ink-2);font-size:11.5px}
  .section-title{display:flex;align-items:center;gap:10px;margin:22px 0 10px;font:750 11px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3)}
  .section-title::after{content:"";height:1px;background:var(--line);flex:1}
  .card{border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);box-shadow:var(--shadow)}
  .card + .card{margin-top:8px}
  .card-h{display:block;padding:12px;border-bottom:1px solid var(--line)}
  .card-h h2,.card-h h3{margin:0;font-size:14.5px;line-height:1.25}
  .card-b{padding:12px}
  .pill{display:inline-flex;align-items:center;gap:6px;border-radius:999px;padding:4px 7px;font:700 9.5px/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;border:1px solid transparent;white-space:nowrap}
  .pill.mini{padding:2px 6px;font-size:9.5px}
  .pill.ok{color:var(--ok);background:var(--ok-bg);border-color:color-mix(in srgb,var(--ok) 18%,transparent)}
  .pill.warn{color:var(--warn);background:var(--warn-bg);border-color:color-mix(in srgb,var(--warn) 18%,transparent)}
  .pill.bad{color:var(--bad);background:var(--bad-bg);border-color:color-mix(in srgb,var(--bad) 18%,transparent)}
  .pill.unknown{color:var(--unknown);background:var(--unknown-bg);border-color:color-mix(in srgb,var(--unknown) 18%,transparent)}
  .pill.supporting{color:var(--accent);background:var(--accent-bg);border-color:color-mix(in srgb,var(--accent) 18%,transparent)}
  .grid-2{display:grid;grid-template-columns:1fr;gap:10px}
  .evidence{margin-top:9px;color:var(--ink-2);font-size:12.5px}
  .evidence b{color:var(--ink)}
  .next{margin-top:10px;padding:9px 10px;border-radius:8px;background:var(--surface-2);border:1px solid var(--line);font-size:12.5px;color:var(--ink-2)}
  ul{margin:8px 0 0;padding-left:18px}li+li{margin-top:6px}
  table{width:100%;border-collapse:collapse;font-size:12px;display:block;overflow-x:auto;-webkit-overflow-scrolling:touch}
  th{color:var(--ink-3);font:700 10px/1 var(--mono);letter-spacing:.07em;text-transform:uppercase;text-align:left}
  th,td{padding:8px 6px;border-bottom:1px solid var(--line);vertical-align:top;min-width:80px}
  td.num{text-align:right;font-variant-numeric:tabular-nums}
  .history,.timeline{display:grid;gap:10px}
  .entry{border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);padding:12px;box-shadow:var(--shadow)}
  .entry .row{display:block}
  .entry h3{margin:0;font-size:14px;line-height:1.25}.entry time{display:block;margin-top:5px;font:520 11px/1.35 var(--mono);color:var(--ink-3);white-space:normal}
  .entry p{margin:8px 0 0;color:var(--ink-2);font-size:12.5px}
  .suggestions{margin-top:12px;display:grid;gap:8px;grid-template-columns:1fr}
  @media (min-width:640px){
    html,body{font-size:15px}
    .wrap{padding:clamp(20px,3vw,32px) clamp(20px,3vw,28px) 64px}
    .top{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;flex-wrap:wrap}
    h1{font-size:30px;line-height:1.05;letter-spacing:-.02em}
    .meta{font-size:12px}
    .status{gap:12px;margin:22px 0 16px;padding:15px 17px}.status .sub{font-size:13px}
    .kpis{margin:16px 0 24px}
    .grid-2{grid-template-columns:repeat(2,minmax(0,1fr))}
    .card-h{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;padding:15px 16px}.card-h h2,.card-h h3{font-size:16px}.card-b{padding:14px 16px}
    table{display:table;font-size:13px}th,td{padding:10px 8px;min-width:0}
    .entry .row{display:flex;justify-content:space-between;gap:10px;align-items:flex-start}.entry h3{font-size:15px}.entry time{margin-top:0;font-size:12px;white-space:nowrap}.entry p{font-size:13.5px}
  }
  @media (min-width:760px){.kpis{grid-template-columns:repeat(4,minmax(0,1fr))}.suggestions{grid-template-columns:repeat(2,minmax(0,1fr))}}
  @media (max-width:360px){.kpis{grid-template-columns:1fr}.hide-sm{display:none}.pill{margin-top:8px}}
</style>
```

### `pulse/goals.html` required structure

`goals.html` is the planned target and current scorecard. It should read as a durable
operating plan, not a daily journal.

Top to bottom:

1. Header and meta.
2. One status banner: the overall goal read.
3. KPI strip: total goals, on-track, at-risk/off-track, unknown/missing evidence.
4. Goal cards, one per goal.
5. Workflow alignment matrix: aligned, supporting, unaligned.
6. Recommendations: the Chief of Staff's open proposals, grouped by goal, newest first.
7. Measurement gaps.
8. Change history, newest first.

Each goal card must include:

- stable `data-goal-id`
- status in `data-status`
- horizon
- target outcome
- KPI targets with stable `data-target-id`: baseline/current value, target value, unit,
  direction, due date, owner, source of truth, and status rule
- contributing workflows
- latest evidence
- confidence
- next action

The **Recommendations** section holds the Chief of Staff's open, proposal-only recommendations
(the org-level ones from the Org Pulse "Generate recommendations" step; per-automation recs live
in each workflow's `builder/improve.html`, not here). Render it as a list of recommendation cards
grouped by goal, **newest first**, and clearly marked as proposals the user/builder decides on —
nothing here is auto-applied. Each recommendation card carries:

- stable `data-rec-id`
- the goal it serves in `data-goal-id` (matching a goal card's `data-goal-id`)
- `data-status` — `proposed` (default), `accepted`, `in_progress`, `needs_evidence`, `done`, `dismissed`, or `blocked`
- `data-impact` and `data-effort` (e.g. `high`/`medium`/`low`)
- a short title, the evidence it rests on, the proposed move, and the expected goal movement

Update an existing recommendation in place (flip `data-status`) instead of duplicating it; keep
accepted/done/dismissed ones for history rather than deleting them. Open statuses are `proposed`,
`accepted`, `in_progress`, `needs_evidence`, and `blocked`; Org Pulse should call out stale open
recommendations before creating new ones for the same goal/gap.

Starter body:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Org Goals</title>
<!-- paste Shared CSS baseline here -->
</head>
<body>
<main class="wrap goals-doc">
  <header class="top">
    <div>
      <div class="eyebrow">chief of staff</div>
      <h1>Org Goals</h1>
      <div class="meta">Updated YYYY-MM-DD HH:MM TZ · review cadence: weekly</div>
    </div>
  </header>

  <section class="status unknown" data-overall-status="unknown">
    <span class="dot"></span>
    <div>
      <div class="text">No measurable org goals have been set yet.</div>
      <div class="sub">Use this page to define outcomes, horizons, measurements, and contributing workflows.</div>
    </div>
  </section>

  <section class="kpis" aria-label="Goal summary">
    <div class="kpi"><div class="label">Goals</div><div class="value">0</div><div class="note">defined</div></div>
    <div class="kpi"><div class="label">On track</div><div class="value">0</div><div class="note">meeting threshold</div></div>
    <div class="kpi"><div class="label">At risk</div><div class="value">0</div><div class="note">needs attention</div></div>
    <div class="kpi"><div class="label">Unknown</div><div class="value">0</div><div class="note">missing evidence</div></div>
  </section>

  <div class="section-title">Goals</div>
  <!-- GOALS: current scorecard -->
  <article class="card goal-card" id="goal-slug" data-goal-id="goal-slug" data-status="unknown">
    <div class="card-h">
      <div>
        <h2><!-- Goal title --></h2>
        <div class="meta">Horizon: <!-- date/range --> · Confidence: <!-- low/medium/high --></div>
      </div>
      <span class="pill unknown">Unknown</span>
    </div>
    <div class="card-b">
      <div class="grid-2">
        <div><b>Target outcome</b><div class="evidence"><!-- concrete outcome --></div></div>
        <div><b>Measurement</b><div class="evidence"><!-- metric + threshold/status rule + source --></div></div>
      </div>
      <table aria-label="Goal targets">
        <thead><tr><th>Target</th><th>Baseline</th><th>Current</th><th>Goal</th><th>Due</th><th>Owner</th></tr></thead>
        <tbody>
          <tr data-target-id="target-slug" data-status="unknown">
            <td><!-- KPI name --><div class="evidence"><!-- source of truth --></div></td>
            <td class="num"><!-- baseline value + unit --></td>
            <td class="num"><!-- latest value + unit --></td>
            <td class="num"><!-- target value + unit --></td>
            <td><!-- YYYY-MM-DD --></td>
            <td><!-- owner --></td>
          </tr>
        </tbody>
      </table>
      <div class="evidence"><b>Contributing workflows:</b> <span data-workflow="Workflow/name">Workflow name</span></div>
      <div class="evidence"><b>Latest evidence:</b> Missing until the contributing workflow reports this metric.</div>
      <div class="next"><b>Next action:</b> Add measurement to the workflow report/evaluation or refine the goal.</div>
    </div>
  </article>

  <div class="section-title">Workflow alignment</div>
  <section class="card">
    <div class="card-b">
      <table>
        <thead><tr><th>Workflow</th><th>Alignment</th><th>Goal / rationale</th><th>Evidence</th></tr></thead>
        <tbody>
          <tr data-workflow="Workflow/name" data-status="unaligned">
            <td>Workflow name</td><td><span class="pill bad">Unaligned</span></td><td>Needs CEO decision</td><td>Not assessed</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <div class="section-title">Recommendations</div>
  <section class="suggestions" aria-label="Chief of Staff recommendations">
    <!-- RECOMMENDATIONS: newest first · proposal-only · grouped by goal -->
    <article class="card rec-card" data-rec-id="rec-slug" data-goal-id="goal-slug" data-status="proposed" data-impact="medium" data-effort="low">
      <div class="card-h">
        <div>
          <h3><!-- Recommendation title --></h3>
          <div class="meta">Goal: <!-- goal title --> · proposed YYYY-MM-DD</div>
        </div>
        <span class="pill unknown">Proposed</span>
      </div>
      <div class="card-b">
        <div class="evidence"><b>Evidence:</b> <!-- runs/reports/tables/Pulse headlines that motivate it --></div>
        <div class="evidence"><b>Proposed move:</b> <!-- new automation / different approach / cross-automation synergy / promotion --></div>
        <div class="evidence"><b>Impact / effort:</b> <span class="pill mini">impact: medium</span> <span class="pill mini">effort: low</span></div>
        <div class="next"><b>Expected movement:</b> <!-- which goal/KPI should move if accepted -->. Proposal only — the user/builder decides.</div>
      </div>
    </article>
  </section>

  <div class="section-title">Measurement gaps</div>
  <section class="card"><div class="card-b">No gaps recorded yet.</div></section>

  <div class="section-title">Change history</div>
  <section class="history">
    <!-- GOAL HISTORY: newest first -->
    <article class="entry" data-date="YYYY-MM-DD">
      <div class="row"><h3>Created scorecard</h3><time>YYYY-MM-DD</time></div>
      <p>Initial goals scorecard created. Add measurable goals before enabling Daily Org Pulse.</p>
    </article>
  </section>
</main>
</body>
</html>
```

### `pulse/org-pulse.html` required structure

`org-pulse.html` is the daily measured narrative. It should answer: what changed, what is
drifting, what was harvested into memory, and what decision should the CEO make next.

Top to bottom:

1. Header and meta.
2. One status banner: the latest org read.
3. KPI strip: goals on-track, workflows broken/drifting, unaligned workflows, suggestions.
4. Newest-first pulse entries.
5. Archive index if the file grows large.

Each daily entry should include:

- goal scorecard summary
- workflow alignment delta
- org health one-liner
- LLM/model tier and cost audit (report-only)
- memory harvested, if any
- suggestion cards, if any

Starter body:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Org Pulse</title>
<!-- paste Shared CSS baseline here -->
</head>
<body>
<main class="wrap pulse-doc">
  <header class="top">
    <div>
      <div class="eyebrow">chief of staff</div>
      <h1>Org Pulse</h1>
      <div class="meta">Latest pass: YYYY-MM-DD HH:MM TZ · scope: all workflows</div>
    </div>
  </header>

  <section class="status unknown" data-overall-status="unknown">
    <span class="dot"></span>
    <div>
      <div class="text">No Org Pulse has run yet.</div>
      <div class="sub">After goals are set, Daily Org Pulse will summarize movement against them.</div>
    </div>
  </section>

  <section class="kpis" aria-label="Org pulse summary">
    <div class="kpi"><div class="label">Goals on track</div><div class="value">0</div><div class="note">of 0</div></div>
    <div class="kpi"><div class="label">Workflow issues</div><div class="value">0</div><div class="note">broken or drifting</div></div>
    <div class="kpi"><div class="label">Unaligned</div><div class="value">0</div><div class="note">needs decision</div></div>
    <div class="kpi"><div class="label">Suggestions</div><div class="value">0</div><div class="note">open</div></div>
  </section>

  <div class="section-title">Latest entries</div>
  <section class="timeline">
    <!-- ORG PULSE ENTRIES: newest first -->
    <article class="entry pulse-entry" data-date="YYYY-MM-DD" data-status="unknown">
      <div class="row"><h3>No goals set</h3><time>YYYY-MM-DD</time></div>
      <p><b>Goal scorecard:</b> No `pulse/goals.html` evidence is available yet.</p>
      <p><b>Org health:</b> Workflow health can be reviewed, but goal progress cannot be measured until goals exist.</p>
      <p><b>LLM/cost audit:</b> No workflow model/cost evidence recorded yet.</p>
      <p><b>Harvested:</b> Nothing written to memory.</p>
      <div class="next"><b>Suggestion:</b> Run `/org-setup` in Chief of Staff to define measurable goals.</div>
    </article>
  </section>

  <div class="section-title">Archive</div>
  <section class="card"><div class="card-b">No archived entries yet.</div></section>
</main>
</body>
</html>
```

### Maintenance rules

- Keep the active file concise. When `org-pulse.html` grows past roughly 20 entries or 70 KB,
  move older routine entries into a dated archive section or `pulse/archive/YYYY-MM.html`,
  leaving recent entries and open suggestions in the active file.
- Never delete goal history unless the CEO explicitly asks. Mark changed goals in history.
- Close or update repeated suggestions instead of duplicating them daily.
- If the source evidence is stale, say so in the evidence line. A stale green status is worse
  than an honest unknown.
- The LLM/cost audit is a reporting section, not a configuration surface. Render it inside the
  daily pulse entry as a compact table or bullets with workflow, configured tier/model, observed
  model, recent cost/tokens, evidence path, and note. Never imply model settings were changed by
  Org Pulse.
