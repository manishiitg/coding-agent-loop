# SparkQuill HTML design system

Every HTML file the app generates (progress reports, academic map, study material,
tests, and anything else) shares this look so they feel like one product. Build a
**complete standalone document** — inline everything, no external assets, fonts,
images, or network calls.

## Rules

- Inline the CSS below in a `<style>` tag; adjust only where a skill asks.
- Warm, calm, encouraging, readable by a child. Rounded cards, generous spacing, one
  clear title with the child's name and date. Only ever real data — never an invented
  score.
- **Make it visually engaging** — children respond to this far more than plain text.
  Use CSS transitions and animation freely: a gentle fade-in on load, hover effects,
  an animated diagram, a subtle progress-fill bar. These are passive: they play on
  their own or on hover, with nothing to click.
- **No form controls at all** — no `<input>`, `<textarea>`, `<select>`, or `<form>`,
  not even an unscripted one. An empty text box is still wrong: the child will type
  into it expecting something to happen, and nothing will. Write "try it yourself"
  questions as plain text with space to work on paper.
  - BAD: `<input type="text" placeholder="Type your answer...">`
  - GOOD: `<p><strong>1. What is 2/5 + 1/5?</strong></p><div class="answer-space"></div>`
- **No click-to-REVEAL** — no `<details>/<summary>`, no tap-to-flip cards. They
  silently show hidden content with no record it happened, so Quill never learns she
  looked or what she guessed. Write the "guess before you peek" moment as plain text
  and let Quill ask for the guess and reveal the answer in chat.
  - BAD: `<details><summary>Reveal the answer</summary><p>Three hearts!</p></details>`
- **Click-to-CHOOSE is welcome — via SQ.choose only.** A button representing a real
  choice (which path, which answer, what next) sends its text to Quill exactly as if
  the child typed it, making the choice a real turn Quill responds to. A button that
  does anything else — toggling visibility, revealing something, nothing at all — is
  as invisible to Quill as a `<details>` reveal, and just as wrong.
  - GOOD: `<button onclick="parent.postMessage({__sq:1,op:'choose',text:'Investigate Saturn'},'*')">Investigate Saturn</button>`
  - BAD: `<button onclick="document.getElementById('a').style.display='block'">Show answer</button>`
- For content that should change turn by turn as the conversation unfolds — rather
  than being fixed at creation time — use `show_scene` instead of this file: a small
  snippet rendered inline in a reply, generated fresh, using the same SQ.choose
  pattern.

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
  .answered-note{color:var(--good);font-size:13px;font-weight:600;margin:8px 0 0}
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
  <script>
    // Lets the app's print icon (outside this sandboxed iframe) trigger a real
    // print of THIS page's own window — a cross-origin frame can postMessage in
    // but cannot call .print() on this window directly, so it asks instead.
    window.addEventListener('message', function (e) {
      if (e && e.data && e.data.__sq === 1 && e.data.op === 'print') window.print()
    })
  </script>
</body>
</html>
```

Use `.card` for each section, `.badge` for marks or a "Current" tag, `.good`/`.focus`
for going-well / to-practise, a `.grid` of `.card`s for the academic map's subjects,
`.note` for honest caveats, and `.answered-note` for the tutor's progress marks.

The parent can print any page from the print icon in the app's viewer — no print
button belongs in the generated page itself.
