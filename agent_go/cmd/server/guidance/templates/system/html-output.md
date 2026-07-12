## HTML Output — High-Quality Self-Contained Reports

Load this doc before any step or agent that writes a `.html` file as a final artifact.

### When to use HTML vs other formats

| Output goes to | Format |
|----------------|--------|
| Downstream step as structured data | **JSON** — always |
| Dedicated HTML surfaces: `builder/improve.html`, org pages (`pulse/*.html`), dashboard cards, published pages | **HTML** — that's what this doc is for |
| Final human-readable report / analysis | **Markdown by default** — reach for HTML only when the layout genuinely needs it (see `code-authoring`); an actual dashboard belongs in the report system (`reports/report_plan.json`), not a hand-authored page |
| Short prose note / KB append / learnings | **Markdown** |
| Raw data the user may download | **JSON or CSV** |

Markdown is the default for human-readable step output — it renders richly in the viewer and gets clickable workspace links. Write HTML when producing one of the dedicated HTML surfaces above or when the user asked for a rich standalone page; then follow every rule below.

### Non-negotiable rules

**Self-contained — no external URLs.**
Every `<style>`, font, icon, and script must be inlined. External CDN links (`<link href="https://...">`, `<script src="https://...">`) break when the file is opened offline or shared. Use `<style>` blocks and `<script>` blocks only. If you need a charting library, write the chart with vanilla JS or inline SVG — a 30-line draw loop is more reliable than any CDN dependency.

**Dark-mode styles.**
Always include:
```html
<style>
  @media (prefers-color-scheme: dark) {
    body { background: #1e1e1e; color: #d4d4d4; }
    table { border-color: #3e3e3e; }
    th { background: #2d2d2d; }
    td { background: #1e1e1e; }
    tr:nth-child(even) td { background: #252526; }
    pre, code { background: #252526; border-color: #3e3e3e; }
    a { color: #4ec9b0; }
  }
</style>
```

**Summary box at the top.**
Every report opens with a `<div class="summary">` showing the key numbers or verdict. The user gets the answer before the detail.

```html
<div class="summary">
  <h2>Summary</h2>
  <div class="stats">
    <div class="stat"><span class="label">Total</span><span class="value">142</span></div>
    <div class="stat"><span class="label">Pass</span><span class="value green">138</span></div>
    <div class="stat"><span class="label">Fail</span><span class="value red">4</span></div>
  </div>
</div>
```

### Layout baseline

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Report Title</title>
<style>
  *, *::before, *::after { box-sizing: border-box; }
  body {
    max-width: 960px; margin: 0 auto; padding: 24px 32px;
    font-family: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    font-size: 15px; line-height: 1.6; color: #1a1a1a; background: #fff;
  }
  h1 { font-size: 1.6rem; margin-bottom: 4px; }
  h2 { font-size: 1.2rem; margin-top: 32px; border-bottom: 2px solid #e0e0e0; padding-bottom: 6px; }
  .meta { color: #666; font-size: 0.85rem; margin-bottom: 24px; }

  /* Summary box */
  .summary { background: #f5f5f5; border-left: 4px solid #007acc; padding: 16px 20px; border-radius: 4px; margin-bottom: 28px; }
  .stats { display: flex; gap: 24px; flex-wrap: wrap; margin-top: 10px; }
  .stat { display: flex; flex-direction: column; }
  .stat .label { font-size: 0.75rem; text-transform: uppercase; color: #666; }
  .stat .value { font-size: 1.4rem; font-weight: 700; }

  /* Semantic colours */
  .green { color: #2e7d32; } .red { color: #c62828; } .amber { color: #e65100; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 0.8rem; font-weight: 600; }
  .badge.pass { background: #e8f5e9; color: #2e7d32; }
  .badge.fail { background: #ffebee; color: #c62828; }
  .badge.warn { background: #fff3e0; color: #e65100; }

  /* Tables */
  table { border-collapse: collapse; width: 100%; margin: 12px 0; }
  th { background: #f0f0f0; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.04em; text-align: left; }
  th, td { padding: 8px 12px; border: 1px solid #e0e0e0; }
  tr:nth-child(even) td { background: #fafafa; }

  /* Code */
  pre, code { background: #f5f5f5; border: 1px solid #e0e0e0; border-radius: 4px; font-family: 'Menlo', 'Consolas', monospace; font-size: 0.85rem; }
  pre { padding: 12px 16px; overflow-x: auto; }
  code { padding: 1px 5px; }
  pre code { border: none; padding: 0; background: none; }

  /* Navigation */
  nav { position: sticky; top: 0; background: #fff; border-bottom: 1px solid #e0e0e0; padding: 8px 0; margin-bottom: 24px; font-size: 0.85rem; z-index: 10; }
  nav a { margin-right: 16px; color: #007acc; text-decoration: none; }
  nav a:hover { text-decoration: underline; }

  @media (prefers-color-scheme: dark) {
    body { background: #1e1e1e; color: #d4d4d4; }
    h2 { border-color: #3e3e3e; }
    .summary { background: #252526; border-color: #007acc; }
    .stat .label { color: #999; }
    .meta { color: #999; }
    .badge.pass { background: #1b3a1f; color: #81c784; }
    .badge.fail { background: #3b1a1a; color: #ef9a9a; }
    .badge.warn { background: #3a2800; color: #ffcc80; }
    table, th, td { border-color: #3e3e3e; }
    th { background: #2d2d2d; }
    td { background: #1e1e1e; }
    tr:nth-child(even) td { background: #252526; }
    pre, code { background: #252526; border-color: #3e3e3e; }
    nav { background: #1e1e1e; border-color: #3e3e3e; }
    a { color: #4ec9b0; }
  }
</style>
</head>
<body>

<h1>Report Title</h1>
<p class="meta">Generated: YYYY-MM-DD HH:MM UTC</p>

<!-- Sticky nav — include only when report has ≥3 sections -->
<nav>
  <a href="#summary">Summary</a>
  <a href="#section-1">Section 1</a>
  <a href="#section-2">Section 2</a>
</nav>

<div id="summary" class="summary">
  <h2>Summary</h2>
  <div class="stats">
    <div class="stat"><span class="label">Metric A</span><span class="value">—</span></div>
    <div class="stat"><span class="label">Metric B</span><span class="value green">—</span></div>
  </div>
</div>

<!-- sections follow -->

</body>
</html>
```

### Inline bar chart (vanilla JS, no CDN)

Use this pattern for any bar or horizontal bar chart. Replace the `data` array with real values from your output.

```html
<canvas id="chart" width="800" height="300" style="max-width:100%;"></canvas>
<script>
(function() {
  var data = [
    { label: 'Jan', value: 42 },
    { label: 'Feb', value: 78 },
    { label: 'Mar', value: 55 }
  ];
  var canvas = document.getElementById('chart');
  var ctx = canvas.getContext('2d');
  var W = canvas.width, H = canvas.height;
  var pad = { top: 20, right: 20, bottom: 40, left: 50 };
  var chartW = W - pad.left - pad.right;
  var chartH = H - pad.top - pad.bottom;
  var max = Math.max.apply(null, data.map(function(d){ return d.value; }));
  var barW = Math.floor(chartW / data.length * 0.6);
  var gap  = Math.floor(chartW / data.length * 0.4);
  var dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  var fg = dark ? '#d4d4d4' : '#333';
  var barColor = '#007acc';

  ctx.clearRect(0, 0, W, H);
  ctx.font = '12px system-ui';
  ctx.fillStyle = fg;

  data.forEach(function(d, i) {
    var x = pad.left + i * (barW + gap);
    var barH = Math.round((d.value / max) * chartH);
    var y = pad.top + chartH - barH;
    ctx.fillStyle = barColor;
    ctx.fillRect(x, y, barW, barH);
    ctx.fillStyle = fg;
    ctx.textAlign = 'center';
    ctx.fillText(d.label, x + barW / 2, H - pad.bottom + 16);
    ctx.fillText(d.value, x + barW / 2, y - 4);
  });

  // Y-axis line
  ctx.strokeStyle = dark ? '#3e3e3e' : '#ccc';
  ctx.beginPath();
  ctx.moveTo(pad.left - 4, pad.top);
  ctx.lineTo(pad.left - 4, pad.top + chartH);
  ctx.stroke();
})();
</script>
```

### Review / findings reports — extra patterns

When the HTML is a review or findings document (not a data dashboard), apply these additional patterns on top of the layout baseline.

**Blocker box — always first inside the summary div.**
Surface the single most important action in a red box before any prose:
```html
<div class="blocker-box">
  <strong>Top blocker:</strong> <code>route-pick-topic</code> validation pins <code>^1\.0$</code>
  but db file is v3.0 — every run fails pre-validation. Fix F-013 first.
</div>
```
```css
.blocker-box {
  background: #fff3f3; border-left: 4px solid #c62828;
  padding: 12px 16px; border-radius: 4px; margin-bottom: 16px; font-size: 0.9rem;
}
@media (prefers-color-scheme: dark) {
  .blocker-box { background: #3b1a1a; border-color: #ff5252; }
}
```

**Stat chips with severity backgrounds — not just colored numbers.**
Each stat chip in the summary gets a tinted background matching its severity:
```html
<div class="stat critical-chip"><span class="label">CRITICAL</span><span class="value">9</span></div>
<div class="stat warning-chip"><span class="label">WARNING</span><span class="value">13</span></div>
<div class="stat info-chip"><span class="label">INFO</span><span class="value">4</span></div>
```
```css
.critical-chip { background: #ffebee; border-radius: 6px; padding: 8px 14px; }
.warning-chip  { background: #fff3e0; border-radius: 6px; padding: 8px 14px; }
.info-chip     { background: #e3f2fd; border-radius: 6px; padding: 8px 14px; }
@media (prefers-color-scheme: dark) {
  .critical-chip { background: #5a2020; }
  .warning-chip  { background: #5a3a00; }
  .info-chip     { background: #1a3a60; }
}
```

**Sticky alert bar — visible while scrolling.**
Add below the `<nav>` so the severity count + top-blocker link stays visible no matter how far the user has scrolled:
```html
<div class="alert-bar">
  <span class="red">9 CRITICAL</span> · <span class="amber">13 WARNING</span> · <span class="blue">4 INFO</span>
  &nbsp;|&nbsp; Top blocker: F-013 &nbsp;
  <a href="#top5">→ Fix first</a>
</div>
```
```css
.alert-bar {
  position: sticky; top: 33px; /* below nav */ background: #fafafa;
  border-bottom: 1px solid #e0e0e0; padding: 6px 16px;
  font-size: 0.82rem; font-weight: 600; z-index: 9;
}
@media (prefers-color-scheme: dark) {
  .alert-bar { background: #252526; border-color: #3e3e3e; }
}
```

**Severity-grouped finding divs — not tables.**
For findings sections, use colored `<div>` containers instead of table rows. Group by severity with a colored header:
```html
<h3 class="severity-heading critical-heading">CRITICAL (9)</h3>
<div class="finding critical">
  <span class="fid">F-2026-05-28-013</span>
  <span class="badge fail">CRITICAL</span>
  <strong>route-design-plan — version contradiction</strong>
  <p>db/card-template-contracts.json is v3.0 but validators pin ^1\.0$...</p>
  <p><em>Action:</em> update regex to <code>^3\.0$</code>. Owner: Pulse Fixer.</p>
</div>

<h3 class="severity-heading warning-heading">WARNING (13)</h3>
<div class="finding warning"> ... </div>
```
```css
.severity-heading { margin-top: 24px; padding: 6px 12px; border-radius: 4px; font-size: 0.9rem; }
.critical-heading { background: #ffebee; color: #c62828; border-left: 4px solid #c62828; }
.warning-heading  { background: #fff3e0; color: #e65100; border-left: 4px solid #e65100; }
.info-heading     { background: #e3f2fd; color: #0277bd; border-left: 4px solid #0277bd; }
@media (prefers-color-scheme: dark) {
  .critical-heading { background: #5a2020; color: #ff8080; }
  .warning-heading  { background: #5a3a00; color: #ffcc80; }
  .info-heading     { background: #1a3a60; color: #90caf9; }
}
```

**Collapsible sections — use `<details>` for phases.**
Wrap each large phase in a `<details>` so users can collapse sections they've read:
```html
<details open>
  <summary class="phase-summary">Phase 2 — Per-Step Audit
    <span class="badge fail" style="float:right">3 CRITICAL</span>
    <span class="badge warn" style="float:right;margin-right:6px">5 WARNING</span>
  </summary>
  <div class="phase-body">
    <!-- findings here -->
  </div>
</details>
```
```css
.phase-summary {
  cursor: pointer; list-style: none; padding: 10px 14px;
  background: #f5f5f5; border-radius: 4px; font-weight: 700;
  font-size: 0.95rem; margin-top: 20px; user-select: none;
}
.phase-summary::-webkit-details-marker { display: none; }
.phase-summary::before { content: "▶ "; font-size: 0.75rem; }
details[open] .phase-summary::before { content: "▼ "; }
.phase-body { padding: 12px 4px; }
@media (prefers-color-scheme: dark) {
  .phase-summary { background: #2d2d2d; }
}
```

**Finding ID dark-mode fix — `.fid` must be visible.**
The `.fid` monospace ID `color: #555` is invisible in dark mode. Always override it:
```css
.fid { font-family: monospace; font-size: 0.8rem; font-weight: 700; color: #555; margin-right: 6px; }
@media (prefers-color-scheme: dark) { .fid { color: #a0c4ff; } }
```

**Badge dark-mode contrast — use these values, not the baseline ones.**
The baseline dark values are too low-contrast for review badges. Use these instead:
```css
@media (prefers-color-scheme: dark) {
  .badge.fail    { background: #5a2020; color: #ff8080; }
  .badge.warn    { background: #5a3a00; color: #ffdb58; }
  .badge.pass    { background: #1b3a1f; color: #80e880; }
  .badge.info    { background: #1a3a60; color: #90caf9; }
  .badge.partial { background: #3a1f60; color: #ce93d8; }
}
```

### Quality checklist — verify before writing the file

- [ ] No external URLs in `<link>` or `<script src>` — all CSS/JS is inline
- [ ] `@media (prefers-color-scheme: dark)` block present with **high-contrast** badge values
- [ ] Summary box at the top with key numbers
- [ ] **Blocker box** (red border, red background) is the first element inside the summary — if there is a top blocker
- [ ] **Stat chips** have severity-tinted backgrounds, not just colored numbers
- [ ] **Sticky alert bar** below the nav shows severity counts + top-blocker link
- [ ] Sticky `<nav>` with anchor links (when ≥3 sections)
- [ ] Tables use `<thead>` + striped rows
- [ ] **Findings use colored `<div>` containers** grouped by severity with severity headings — not flat table rows
- [ ] **Phases wrapped in `<details open>`** so users can collapse sections
- [ ] **`.fid` IDs have dark-mode override** (`color: #a0c4ff`)
- [ ] Status fields use `.badge.pass` / `.badge.fail` / `.badge.warn` classes with high-contrast dark values
- [ ] No raw JSON blobs visible as text — data embedded in JS variables
- [ ] `<meta viewport>` present for responsive layout
- [ ] File is self-contained: opening it with no network renders correctly
