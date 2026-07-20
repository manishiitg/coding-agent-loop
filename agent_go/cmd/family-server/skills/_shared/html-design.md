# SparkQuill HTML design system

Every HTML file the app generates (progress reports, academic map, study
material, tests, and any other) MUST share this look so they feel like one
product. Build a **complete standalone document** — inline everything, NO
external assets, fonts, images, or network calls.

## Rules
- Inline the CSS below in a `<style>` tag (adjust only where a skill asks).
- **View-only, static HTML.** Do not add `<script>` for typed-answer capture,
  auto-save, or any input the file itself remembers — no forms, no JS state.
  It is a clean, well-designed document to read, not an app.
- Warm, calm, encouraging — readable by a child. Never harsh.
- Rounded cards, generous spacing, one clear title with the child's name + date.
- Use only real data. Never invent scores.

## Base template

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title><!-- e.g. Maya — Progress --></title>
<style>
  :root{
    --bg:#fbf7ef; --ink:#16223a; --muted:#5b6b86; --sun:#f6b93b;
    --sun-soft:#fdeecb; --card:#ffffff; --line:#ece3d2; --good:#2f9e6f; --focus:#e08a3c;
  }
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--ink);
    font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;padding:18px 22px}
  .wrap{max-width:840px;margin:0 auto}
  .head{display:flex;align-items:center;gap:10px;margin-bottom:14px}
  .head .sun{width:30px;height:30px;border-radius:50%;background:var(--sun);
    display:grid;place-items:center;font-size:16px;flex:0 0 auto}
  h1{font-size:19px;margin:0;line-height:1.2}
  .sub{color:var(--muted);font-size:14px;margin-top:2px}
  .card{background:var(--card);border:1px solid var(--line);border-radius:16px;
    padding:20px 22px;margin:16px 0;box-shadow:0 2px 10px rgba(22,34,58,.05)}
  .card h2{font-size:13px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin:0 0 12px}
  .badge{display:inline-block;background:var(--sun-soft);color:#8a6114;font-size:12px;
    font-weight:700;padding:3px 10px;border-radius:999px}
  .good{color:var(--good);font-weight:600}
  .focus{color:var(--focus);font-weight:600}
  ul{margin:8px 0;padding-left:20px} li{margin:5px 0}
  .grid{display:grid;gap:14px;grid-template-columns:repeat(auto-fill,minmax(220px,1fr))}
  .note{background:var(--sun-soft);border-radius:12px;padding:12px 16px;color:#6f5a2a;font-size:14px;margin-top:14px}
  .foot{color:var(--muted);font-size:13px;margin-top:26px;text-align:center}
</style>
</head>
<body>
  <div class="wrap">
    <div class="head">
      <span class="sun">☀</span>
      <div><h1><!-- title with child name --></h1><div class="sub"><!-- subject / date --></div></div>
    </div>
    <!-- cards go here -->
    <div class="foot">SparkQuill · generated from <child>’s workspace</div>
  </div>
</body>
</html>
```

Use `.card` for each section, `.badge` for "Current", `.good`/`.focus` for going-well / to-practise,
`.grid` of `.card`s for the academic map's subjects, and `.note` for honest caveats.

A test is still a clean, well-formatted question sheet — numbered questions,
marks as a `.badge`, blank space (or a printed line) under each question for
working — it is just static: no answer box the page itself remembers. The
child answers on paper or tells Quill in chat; that's how their work reaches
`child/attempts/`.
