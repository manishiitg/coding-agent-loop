## HTML Output — High-Quality Self-Contained Reports

Load this doc before any step or agent that writes a `.html` file as a final artifact.

### When to use HTML vs other formats

| Output goes to | Format |
|----------------|--------|
| Downstream step as structured data | **JSON** — always |
| Final human-readable report / analysis / dashboard | **HTML** — always |
| Short prose note / KB append / learnings | **Markdown** |
| Raw data the user may download | **JSON or CSV** |

Never write Markdown for a final report that a human will open in the viewer. HTML renders richly; Markdown is a plain-text fallback.

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

### Quality checklist — verify before writing the file

- [ ] No external URLs in `<link>` or `<script src>` — all CSS/JS is inline
- [ ] `@media (prefers-color-scheme: dark)` block present
- [ ] Summary box at the top with key numbers
- [ ] Sticky `<nav>` with anchor links (when ≥3 sections)
- [ ] Tables use `<thead>` + striped rows
- [ ] Status fields use `.badge.pass` / `.badge.fail` / `.badge.warn` classes
- [ ] No raw JSON blobs visible as text — data embedded in JS variables
- [ ] `<meta viewport>` present for responsive layout
- [ ] File is self-contained: opening it with no network renders correctly
