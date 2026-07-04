## Chief Scheduled Tasks Report

Normal Chief of Staff schedules write their durable result into one shared HTML file:
`pulse/task.html`.

This is separate from Org Pulse. Org Pulse is the daily org heartbeat. `pulse/task.html`
is the run ledger for recurring Chief of Staff tasks that are neither Org Pulse nor memory
enrichment: reports requested by the user, cross-workflow recommendation scans, recurring
reviews, and similar scheduled work.

### Contract

- Use exactly one file: `pulse/task.html`. Do not create per-task HTML files.
- Newest run first. Prepend each completed task run after
  `<!-- CHIEF TASK ENTRIES: newest first -->`.
- Keep it self-contained: inline CSS, no CDN, no remote fonts, no external images.
- Mobile/right-panel first. It must read well in the Chief of Staff right panel and also full width.
- Do not write to `pulse/org-pulse.html`, `pulse/goals.html`, workflow folders, memory, or schedules
  from this report-update turn.
- Do not redo the task. Summarize the just-completed scheduled run from the current conversation.
- Include concrete evidence paths or links when the task touched workflow files, reports, DB rows,
  or generated outputs. If no file evidence exists, say "conversation result".
- Do not include secrets, raw logs, tokens, or long transcripts.

### What Each Entry Must Capture

Each scheduled task run entry should include:

- schedule name and schedule id
- run id and session id
- status: `success`, `error`, or `unknown`
- started/completed timestamp and duration when provided
- original task/request in one sentence
- result summary
- recommendations, decisions, or findings
- affected workflows/entities, if any
- evidence paths
- next action / owner

Use stable attributes for parsing:

- `.task-entry`
- `data-schedule-id`
- `data-run-id`
- `data-status`
- `data-date`

### Starter Structure

When `pulse/task.html` does not exist, create it from this shape and then insert the first entry.
When it exists, preserve prior entries and update only the header summary plus newest entry.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chief Tasks</title>
<style>
  :root{
    color-scheme:light;--bg:#f7f7f4;--surface:#fff;--surface-2:#fbfaf7;
    --ink:#191917;--ink-2:#595852;--ink-3:#8a887f;--line:#ebe8df;--line-2:#ded9cf;
    --ok:#3f7a4a;--ok-bg:#eaf3ea;--warn:#9a6a05;--warn-bg:#fbf1d8;
    --bad:#b23a2f;--bad-bg:#fbe8e4;--accent:#5b6f95;--accent-bg:#e9eef8;
    --shadow:0 1px 2px rgba(20,20,18,.04),0 8px 24px -16px rgba(20,20,18,.18);
    --sans:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
    --mono:"SF Mono",ui-monospace,Menlo,Consolas,monospace;--r:8px;
  }
  html[data-theme="dark"],html.dark{
    color-scheme:dark;--bg:#0f0f12;--surface:#17171c;--surface-2:#121217;
    --ink:#f1f0f4;--ink-2:#aaa8b1;--ink-3:#74727d;--line:#292832;--line-2:#373541;
    --ok:#62d58b;--ok-bg:#10281a;--warn:#e8b85a;--warn-bg:#2a210d;
    --bad:#f08078;--bad-bg:#311815;--accent:#9eb6e8;--accent-bg:#151f31;
    --shadow:0 1px 0 rgba(255,255,255,.04) inset,0 10px 30px -18px rgba(0,0,0,.8);
  }
  *{box-sizing:border-box} html,body{margin:0;min-height:100%;width:100%;max-width:100%;overflow-x:hidden;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:14px;line-height:1.5}
  body,.card,.entry,.kpi,td,th{overflow-wrap:anywhere}
  .wrap{width:100%;max-width:980px;margin:0 auto;padding:12px 10px 48px}
  .eyebrow{font:700 10px/1 var(--mono);letter-spacing:.13em;text-transform:uppercase;color:var(--ink-3)}
  h1{margin:6px 0 0;font-size:22px;line-height:1.08}
  .meta{margin-top:8px;color:var(--ink-3);font:520 11px/1.45 var(--mono)}
  .status,.entry,.card,.kpi{border:1px solid var(--line);border-radius:var(--r);background:var(--surface);box-shadow:var(--shadow)}
  .status{margin:14px 0 12px;padding:11px 12px}
  .text{font-weight:750}.sub{margin-top:3px;color:var(--ink-2);font-size:12px}
  .kpis{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:12px 0}
  .kpi{padding:10px}.label{color:var(--ink-3);font:700 10px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase}.value{margin-top:5px;font-size:20px;font-weight:800}.note{color:var(--ink-2);font-size:11px}
  .section-title{margin:16px 0 8px;color:var(--ink-3);font:800 10px/1 var(--mono);letter-spacing:.12em;text-transform:uppercase}
  .timeline{display:grid;gap:10px}.entry{padding:12px}.row{display:flex;flex-wrap:wrap;gap:6px 10px;align-items:flex-start;justify-content:space-between}.row h3{margin:0;font-size:15px}.row time{color:var(--ink-3);font:650 11px/1.4 var(--mono)}
  .pill{display:inline-flex;align-items:center;border-radius:999px;padding:3px 8px;font:750 10px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase}
  .ok{color:var(--ok);background:var(--ok-bg)}.warn{color:var(--warn);background:var(--warn-bg)}.bad{color:var(--bad);background:var(--bad-bg)}.info{color:var(--accent);background:var(--accent-bg)}
  .grid{display:grid;gap:8px;margin-top:10px}.field{padding:8px;border:1px solid var(--line);border-radius:6px;background:var(--surface-2)}.field b{display:block;color:var(--ink-3);font:750 10px/1.3 var(--mono);letter-spacing:.08em;text-transform:uppercase}.field span,.field p{display:block;margin:4px 0 0;color:var(--ink)}
  a{color:inherit}
  @media (min-width:680px){.wrap{padding:18px 18px 56px}.kpis{grid-template-columns:repeat(4,minmax(0,1fr))}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
</head>
<body>
<main class="wrap task-doc">
  <header>
    <div class="eyebrow">chief of staff</div>
    <h1>Tasks</h1>
    <div class="meta">Latest update: YYYY-MM-DD HH:MM TZ</div>
  </header>
  <section class="status" data-overall-status="unknown">
    <div class="text">No scheduled task runs recorded yet.</div>
    <div class="sub">Normal Chief of Staff schedules appear here after they complete.</div>
  </section>
  <section class="kpis" aria-label="Task summary">
    <div class="kpi"><div class="label">Runs</div><div class="value">0</div><div class="note">recorded</div></div>
    <div class="kpi"><div class="label">Success</div><div class="value">0</div><div class="note">latest window</div></div>
    <div class="kpi"><div class="label">Issues</div><div class="value">0</div><div class="note">need review</div></div>
    <div class="kpi"><div class="label">Next actions</div><div class="value">0</div><div class="note">open</div></div>
  </section>
  <div class="section-title">Latest task runs</div>
  <section class="timeline">
    <!-- CHIEF TASK ENTRIES: newest first -->
  </section>
</main>
</body>
</html>
```
