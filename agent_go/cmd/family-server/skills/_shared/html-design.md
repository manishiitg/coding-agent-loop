# SparkQuill HTML design system

Every HTML file the app generates (progress reports, academic map, study
material, tests, and any other) MUST share this look so they feel like one
product. Build a **complete standalone document** — inline everything, NO
external assets, fonts, images, or network calls.

## Rules
- Inline the CSS below in a `<style>` tag (adjust only where a skill asks).
- A small **inline** `<script>` for interactivity (reveal a worked solution,
  check a typed answer, toggle a hint) is allowed and encouraged where it helps
  learning — self-contained only, never an external `src`.
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

## Persisting what the child does (tests, quizzes)

For interactive HTML that should REMEMBER the child's input (typed answers, quiz
progress, self-check results), include this helper verbatim and use it:

```html
<script>
window.SQ=(function(){
  function send(m){parent.postMessage(Object.assign({__sq:1},m),'*');}
  return{
    save:function(key,data){send({op:'save',key:key,data:data});},
    load:function(key){return new Promise(function(res){
      var id=String(Math.random()).slice(2);
      function h(e){if(e.data&&e.data.__sq===1&&e.data.op==='loaded'&&e.data.id===id){window.removeEventListener('message',h);res(e.data.data);}}
      window.addEventListener('message',h);send({op:'load',key:key,id:id});
    });}
  };
})();
</script>
```

- `SQ.save(key, data)` — persist any JSON value under `key`. Call it whenever the
  child changes something (types an answer, submits a question, finishes a quiz).
- `SQ.load(key).then(function(data){ ... })` — on page load, restore saved state
  (`data` is `null` if nothing was saved yet); re-fill the inputs from it.
- Choose ONE stable `key` that identifies this file — use the file's own name,
  e.g. `"2026-07-20-quadratics-practice"`. Keep it identical for save and load.

The app writes this to the child's workspace (`child/attempts/<key>.json`), so it
survives reloads AND Quill can read the child's actual answers later to mark them
and give feedback. Every test you create MUST record the child's answers this way.
