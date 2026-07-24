# SparkQuill HTML design system

Every HTML file the app generates (progress reports, academic map, study
material, tests, and any other) MUST share this look so they feel like one
product. Build a **complete standalone document** — inline everything, NO
external assets, fonts, images, or network calls.

## Rules
- Inline the CSS below in a `<style>` tag (adjust only where a skill asks).
- **View-only, static HTML.** No `<input>`, `<textarea>`, `<select>`, or `<form>`
  elements AT ALL — not even a plain, unscripted one. This is not just "no
  auto-save JS": an empty text box with no script behind it is STILL wrong,
  because the child will type into it expecting something to happen and
  nothing will. If a page needs a "try it yourself" section, write the
  question as plain text with blank space below it to work on paper — never
  an on-screen box that looks fillable.
  - BAD (never do this): `<input type="text" placeholder="Type your answer...">`
  - GOOD: `<p><strong>1. What is 2/5 + 1/5?</strong></p><div class="answer-space"></div>`
    (an empty `<div>` styled with height/border, not a form control)
  It is a clean, well-designed document to read, not an app.
- Warm, calm, encouraging — readable by a child. Never harsh.
- Rounded cards, generous spacing, one clear title with the child's name + date.
- Use only real data. Never invent scores.
- **Make it visually engaging — children respond to this far more than plain text.**
  Use CSS transitions/animations freely: a gentle fade/slide-in as the page loads,
  hover effects on cards, an animated diagram or icon, a subtle progress-fill bar.
  These are passive/decorative — they play automatically or on hover, nothing to
  click.
- **No click-to-REVEAL elements — no `<details>/<summary>`, no "tap to flip"
  cards.** These silently show hidden content (a hint, an answer, a fun fact)
  with no record of it happening — Quill never finds out the child looked, or
  what she was even looking at. Write a "guess before you peek" moment as
  plain text instead and let Quill prompt for the guess and reveal the answer
  itself in chat.
  - GOOD: `<p><strong>Guess: how many hearts does an octopus have?</strong></p>` (no reveal element) — Quill asks in chat, then reveals the fact in its own reply.
  - BAD: `<details><summary>Reveal the answer</summary><p>Three hearts!</p></details>` — Quill never finds out she looked, or what she guessed.
- **Click-to-CHOOSE elements are fine — but MUST use SQ.choose so Quill actually
  sees the pick.** A page isn't stuck being non-interactive: a button
  representing a genuine choice (which path, which answer, what to do next)
  can call `parent.postMessage({__sq:1,op:'choose',text:'<exact message>'},'*')`
  in its `onclick` — this sends that text to Quill exactly as if the child
  typed it, so the choice becomes a real turn Quill responds to. A button
  that does anything else (toggles local visibility, does nothing, or just
  silently reveals something) is exactly as invisible to Quill as a
  `<details>` reveal and is just as wrong.
  - GOOD: `<button onclick="parent.postMessage({__sq:1,op:'choose',text:'Investigate Saturn'},'*')">Investigate Saturn</button>`
  - BAD: `<button onclick="document.getElementById('a').style.display='block'">Show answer</button>` — Quill never knows this happened.
- For content that changes turn-by-turn as the conversation actually unfolds
  (not fixed at creation time) — see `show_scene` in your own instructions:
  a small HTML snippet shown inline in a reply, generated fresh each time,
  which can use the same SQ.choose pattern above.

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

A parent can print any HTML page from the print icon in the app's viewer (next to
refresh) — no button needs to be built into the generated page itself.

Use `.card` for each section, `.badge` for "Current", `.good`/`.focus` for going-well / to-practise,
`.grid` of `.card`s for the academic map's subjects, and `.note` for honest caveats.

A test is still a clean, well-formatted question sheet — numbered questions,
marks as a `.badge`, blank space (or a printed line) under each question for
working — it is just static: no answer box the page itself remembers. The
child answers on paper or tells Quill in chat; that's how their work reaches
their activity's own `attempts/` folder, and (editing the real file directly,
via their shell — see childSystemPrompt) how a small `.answered-note` line
ends up on the page itself, right under the question it belongs to.
