# SparkQuill HTML design system

> Activity pages are Quill's own design now (see the parent prompt); this
> guide is for the progress page.

Every HTML file the app generates (the progress page, study material,
tests, and anything else) shares this look so they feel like one product. Build a
**complete standalone document** — inline the CSS, no web fonts, no hotlinked
images, no network calls at load time.

The one thing that may live beside the page rather than inside it is a picture —
fetched by `find_image` or made with `image_gen`/`image_edit` (see "Real pictures"
and "AI-generated pictures" below). It sits in the same folder and is referenced
with a plain relative `<img src="filename.png">`; the app resolves that when it
displays the page. Never reference a URL on the internet directly — that breaks
the moment the page is printed or opened offline.

## Rules

- Inline the CSS below in a `<style>` tag; adjust only where a skill asks.
- Warm, calm, encouraging, readable by a child. Rounded cards, generous spacing, one
  clear title with the child's name and date. Only ever real data — never an invented
  score.
- **Make it visually engaging** — children respond to this far more than plain text.
  You are not limited to a fixed list of effects: use whatever CSS (transitions,
  keyframe animation, transforms) genuinely brings a page to life, at whatever
  richness the moment deserves. Judge tastefully — quality over quantity, and
  never at the cost of the content being clear and correct — but don't hold back
  on capability you actually have.
- **No form controls at all** — no `<input>`, `<textarea>`, `<select>`, or `<form>`,
  not even an unscripted one. An empty text box is still wrong: the child will type
  into it expecting something to happen, and nothing will. Write "try it yourself"
  questions as plain text with space to work on paper.
  - BAD: `<input type="text" placeholder="Type your answer...">`
- **Give every major section, sub-section, and figure a real, predictable `id`** —
  not just questions. `open_file`'s `focus` parameter scrolls the page straight to
  any id on it, so when you're talking about "the worked example" or "Figure 2" the
  page can actually jump there instead of leaving her to scroll and hunt while you're
  mid-explanation. One scheme, applied consistently in document order:
  - Each major section (a `.card`): `id="s1"`, `id="s2"`, ...
  - A sub-section inside one (a sub-heading, a worked example): `id="s2-1"`, `id="s2-2"`, ...
  - Each figure (a `.fig`, including a `find_image` or `image_gen` picture): `id="fig1"`, `id="fig2"`, ...
  - GOOD: `<div class="card" id="s2"><h2>Worked examples</h2><div id="s2-1">...</div></div>`
  You wrote these ids, so you know them — reference them later without re-reading the
  file. A turn that's clearly ABOUT one section or figure and doesn't pass focus is a
  missed opportunity, the same way naming a question by number without focus is.
- **Wrap every question in `<div class="q" id="q1">`**, the id's number matching the
  question's own. This is load-bearing beyond ordinary addressability: `open_file`
  with NO `focus` lands the page at the first `.q` that has no answer recorded inside
  it, so the child lands on the question she's up to instead of back at the top every
  time the tutor records something. A question outside a `.q` is invisible to that,
  and the page keeps reopening at the top.
  - GOOD:
    ```html
    <div class="q" id="q1">
      <p><strong>1. What is 2/5 + 1/5?</strong> <span class="badge">2 marks</span></p>
      <div class="answer-space"></div>
    </div>
    ```
- **`.answer-space` is the blank for working on paper, and belongs to an UNANSWERED
  question only.** When the tutor records an answer it replaces that question's
  `.answer-space` with the `.answered-note`. Leaving both makes a finished question
  look unfinished and invites her to answer it twice.
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
  Always through `SQ.choose(text, this)` (defined once in the base template's own
  `<script>`, below) — never a raw inline `postMessage` — because it also disables
  the button the instant it's clicked, so a slow reply can't be mistaken for a
  missed tap and answered twice.
  - GOOD: `<button onclick="SQ.choose('Investigate Saturn', this)">Investigate Saturn</button>`
  - BAD: `<button onclick="parent.postMessage({__sq:1,op:'choose',text:'Investigate Saturn'},'*')">Investigate Saturn</button>` — sends correctly, but the button stays clickable
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
    font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;padding:14px 16px}
  .wrap{max-width:1100px;margin:0 auto}
  .head{display:flex;align-items:center;gap:10px;margin-bottom:14px}
  .head .sun{width:30px;height:30px;border-radius:50%;background:var(--sun);
    display:grid;place-items:center;font-size:16px;flex:0 0 auto}
  h1{font-size:19px;margin:0;line-height:1.2}
  .sub{color:var(--muted);font-size:14px;margin-top:2px}
  .card{background:var(--card);border:1px solid var(--line);border-radius:16px;
    padding:16px 18px;margin:12px 0;box-shadow:0 2px 10px rgba(22,34,58,.05)}
  .card h2{font-size:13px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin:0 0 12px}
  .badge{display:inline-block;background:var(--sun-soft);color:#8a6114;font-size:12px;
    font-weight:700;padding:3px 10px;border-radius:999px}
  .good{color:var(--good);font-weight:600}
  .focus{color:var(--focus);font-weight:600}
  ul{margin:8px 0;padding-left:20px} li{margin:5px 0}
  .grid{display:grid;gap:14px;grid-template-columns:repeat(auto-fill,minmax(220px,1fr))}
  .note{background:var(--sun-soft);border-radius:12px;padding:12px 16px;color:#6f5a2a;font-size:14px;margin-top:14px}
  /* Deliberately NEUTRAL — not green, no tick. This records WHAT she answered,
     never whether it was right; a green tick was being read as "correct" by both
     parent and child. Reads as a pencil note in the margin. */
  .answered-note{color:var(--muted);font-size:13px;margin:8px 0 0;
    padding:7px 11px;background:#f4f1ea;border-left:3px solid #d9d2c4;border-radius:8px}
  .foot{color:var(--muted);font-size:13px;margin-top:26px;text-align:center}
  /* --- showing data: a meter row + status chip. See "Showing data" below. --- */
  .rows{display:flex;flex-direction:column;gap:10px;margin:10px 0}
  .row{display:grid;grid-template-columns:1fr auto;gap:4px 12px;align-items:baseline}
  .row .name{font-size:14px;font-weight:600}
  .row .val{font-size:13px;color:var(--muted);font-variant-numeric:tabular-nums}
  /* Track + fill. The fill is a thin mark anchored to the left with rounded
     data-ends, per the shared mark spec. Width is set inline as a percentage. */
  .meter{grid-column:1/-1;height:9px;border-radius:999px;background:#efe7d8;overflow:hidden}
  .meter>span{display:block;height:100%;border-radius:999px;background:var(--good);
    /* grows on load so the page feels alive without anything to click */
    animation:grow .8s ease-out both}
  .meter.is-focus>span{background:var(--focus)}
  .meter.is-none>span{background:#c9d0dc}
  @keyframes grow{from{width:0 !important}}
  @media (prefers-reduced-motion:reduce){.meter>span{animation:none}}
  /* Status chip. ALWAYS carries its word + mark — never colour alone. */
  .chip{display:inline-flex;align-items:center;gap:5px;font-size:12px;font-weight:700;
    padding:3px 10px;border-radius:999px;background:#eaf7ee;color:#26765a}
  .chip.is-focus{background:var(--sun-soft);color:#8a6114}
  .chip.is-none{background:#eef1f6;color:#5c6d88}
  /* Set by SQ.choose the instant a choice button is clicked — see below. */
  button:disabled{opacity:.55;cursor:default}
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
    // SQ.choose is the ONLY way a click-to-choose button should send its turn —
    // see "Click-to-CHOOSE" below. Disables the button THE INSTANT it's
    // clicked, before the message even reaches the app: a real reply can take a
    // minute or more, and a button that stays clickable invites tapping it
    // again, which sends the exact same answer as a second, duplicate turn —
    // confirmed live, a child tapped one five-plus times waiting for a
    // response, each tap queued as its own separate turn behind the last.
    // Defined once here rather than left to each button's own onclick so
    // every generated page gets this for free, consistently, rather than
    // depending on it being hand-written correctly every time.
    window.SQ = {
      choose: function (text, el) {
        if (el && el.disabled) return
        if (el) el.disabled = true
        parent.postMessage({ __sq: 1, op: 'choose', text: text }, '*')
      }
    }
  </script>
</body>
</html>
```

Use `.card` for each section, `.badge` for marks or a "Current" tag, `.good`/`.focus`
for going-well / to-practise, a `.grid` of `.card`s for the subject cards,
`.note` for honest caveats, and `.answered-note` for the tutor's progress marks.

`.answered-note` records WHAT she answered and never whether it was right. Write it
as `✎ Answered: <em>her words</em>` — a pencil, never a tick, and never green. A tick
and a green note were both being read as "correct" by the parent AND the child, which
quietly turns a neutral record into a grade the tutor never gave — and during a real
assessment the tutor must not reveal correctness at all. Marking is the parent's
job, from the answer key.

## Real pictures

Some things are far easier to understand from a real picture than from any
description: what a plateau actually looks like, where the Tropic of Cancer falls
on a world map, how the digestive system is arranged. `find_image` fetches one
from Wikimedia Commons, saves it beside the page, and hands back the filename plus
the credit to print under it.

- **Use it where seeing the thing IS the teaching** — a real landform, a map, a
  labeled biological diagram, a historical photograph.
- **Not for what you can draw better yourself.** A relationship, a process, a
  comparison, a bar of progress — a drawn figure is sharper, scales cleanly, and
  can use the page's own colours. A fetched picture is for things that genuinely
  exist in the world.
- **For geometry, graphs and number lines, read `skills/guides/diagrams.md`
  first.** An angle, a circle, a labelled triangle, a bar chart she reads values
  off — those are declared with JSXGraph (already available on every page), never
  hand-written as SVG coordinates. Hand-computed geometry gets the y-axis
  direction, the arc flags and the label placement wrong, and a wrong figure
  teaches the wrong thing.
- **Reference it relatively and give it real alt text**, describing what it shows
  rather than repeating the caption:
  ```html
  <figure class="fig" id="fig1">
    <img src="latitude-map.png" alt="World map with the Equator, both Tropics and the polar circles marked">
    <figcaption>The main latitude circles · Thesevenseas · CC BY-SA 3.0</figcaption>
  </figure>
  ```
- **Always print the credit** from the tool's `attribution` field in the
  `figcaption`. These images are openly licensed, which is what makes them safe to
  include in a page the parent may share — attribution is the condition of that.
- **Never invent a filename.** Only reference what `find_image` actually returned;
  if it found nothing, write the page without a picture rather than leaving a
  broken image on a child's screen.

Add this to the CSS when a page uses pictures:

```css
.fig{margin:14px 0;text-align:center}
.fig img{max-width:100%;height:auto;border-radius:12px;border:1px solid var(--line)}
.fig figcaption{margin-top:6px;color:var(--muted);font-size:12px}
```

## AI-generated pictures

Some things are best shown with an illustration that doesn't exist anywhere to
photograph or fetch: a friendly custom scene for a story activity, a character
reacting to what she just got right, a stylised diagram that needs an artistic
touch beyond what CSS or JSXGraph can give it. `image_gen` makes one from a text
description and saves it to a file; `image_edit` revises an existing one — hers,
yours, or one it just made. Both need a full absolute output path under the
activity's own folder, not a workspace-relative one — `pwd` (from
`execute_shell_command`) plus `/generated-images/<name>.png` gets you there.

- **Use it where the image needs to be MADE, not found or drawn precisely.** A
  whimsical scene, a friendly mascot, an illustration for a story beat, a
  stylised cover image — the same instinct as `find_image`'s "seeing the thing
  is the teaching," but for things that only exist as an illustration.
- **Not for geometry, graphs, real places or things, or anything precision
  matters for.** A generated figure can't be trusted to place points, labels or
  proportions correctly — JSXGraph (`diagrams.md`) or `find_image` are still the
  right tool for those, exactly as above.
- **Keep it in the page's own warm, friendly style** — bright, uncluttered,
  age-appropriate, nothing scary or that reads like stock-photo art clashing
  with the rest of a handwritten-notebook-feeling page.
- **Embed it the same way as a fetched picture** — same `<figure class="fig">`
  wrapper and real alt text; no caption/credit needed, unlike a fetched photo,
  since there's nothing to attribute:
  ```html
  <figure class="fig" id="fig2">
    <img src="generated-images/moon-rock-lab.png" alt="A friendly cartoon Moon rock glowing faintly on a lab table">
  </figure>
  ```
- **One generation per idea, not a retry loop chasing perfection.** If the
  first result is usable, use it — a study guide's job is the learning, not a
  flawless illustration.

## Showing data

Prefer something a parent can SEE over a paragraph they have to read. Where a page
reports how things stand per subject or topic, lead with the visual and let prose
explain only what a picture can't.

Aim for a warm **teacher's report to a family** — not a business dashboard. The
child reads these pages too, so keep the feel of something a favourite teacher
wrote: a subject icon or emoji beside each heading, rounded cards, a kind sentence
next to the numbers, and the child addressed by name where it fits. Imaginative and
encouraging beats clinical and dense. What must stay strict is the *honesty* of the
numbers, never the tone around them: no KPI walls, no grades-as-verdict, no red.

- **A meter row per topic** is the workhorse — name, a short value, and a bar:
  ```html
  <div class="row"><span class="name">Fractions</span><span class="val">2 of 8 · needs work</span>
    <span class="meter is-focus"><span style="width:25%"></span></span></div>
  ```
  Wrap a set in `<div class="rows">`. Bars sharing a card must share one scale, so
  their lengths are comparable at a glance.
- **Three states only**, each with its own class AND its own word: secure
  (`.meter`/`.chip`, "confident"), needs work (`.is-focus`), not started
  (`.is-none`). Never a fourth colour.
- **Always label the number.** Every bar carries its real value as text, and every
  chip carries its word — never colour alone. This is not decoration: the green and
  amber are only ΔE 6.2 apart for red-blind (protanopia) readers, measured, so the
  colour is a hint and the text is the actual information. A bar with no number is a
  bug.
- **Never invent a number to make a bar.** A bar needs a real count ("2 of 8
  attempted"). With no evidence, use `.is-none` and the word "not started" — an
  honest empty state beats a fabricated 40%.
- **No pie or donut charts** (angles are hard to compare, and topic counts are
  usually too small to be meaningful), and never two different scales in one chart.
  A simple bar or a count is almost always the right answer here.
- Motion is passive and already handled: bars grow on load, and reduced-motion is
  respected. Nothing here is clickable, so none of the SQ.choose rules apply.

The parent can print any page from the print icon in the app's viewer — no print
button belongs in the generated page itself.

## Check a figure before you finish

**Only when the page you just wrote contains a geometric figure, graph or chart**
(see `skills/guides/diagrams.md`) — plain prose, question and study pages need
none of this, so don't spend the time on them.

A figure is the one thing on a page you cannot verify by re-reading your own
markup: it either renders correctly or it silently doesn't, and a wrong or blank
figure teaches the wrong thing. So look at it:

1. Open the page with `agent_browser` at
   `http://127.0.0.1:8010/api/workspace/raw?path=<the page's workspace-relative path>`
   (URL-encode the path) and take a `screenshot`.

   **Never open it as a `file://` path.** The page carries only the figure's
   own few lines; the drawing library is supplied by the server on that URL. A
   `file://` copy has no library, so EVERY figure looks like an empty box and
   the console says "JXG is not defined" — you would be staring at a fault in
   how you opened it, not in your figure, and "fixing" geometry that was
   already right. (Port 8010 is the default; if it isn't reachable, skip this
   check and finish rather than retrying — a page with an unverified figure is
   better than no page.)
2. `read_image` the screenshot.
3. Check the things that actually go wrong: is the figure THERE at all (not a
   blank box)? Are the labels on the right points — is the vertex of ∠ABC really
   at B? Does anything overlap or sit outside the frame? Do the bars match the
   axis values they're supposed to show?
4. Fix and re-check if something is wrong. If it's still wrong after a couple of
   rounds, simplify the figure rather than shipping a broken one — a plainer
   figure that is correct beats an ambitious one that isn't.

Never tell the child or the parent that you did this, and never mention
screenshots or files — it is your own check, part of writing the page.
