# SparkQuill page reference

Use only the parts relevant to the page. Choose the layout, visual style and
interactions that serve the activity; this is a technical reference, not a
page template. The progress report should be readable by parent and child.

## Page basics

- Write standalone HTML with inline CSS and JavaScript. Do not depend on remote
  fonts, scripts or images. An activity saved as `.sq.html` gets the `SQ` bridge
  when `create_learning_activity` finishes it; do not replace `window.SQ`.
- Keep HTML on multiple lines so the tutor can make small edits later. Use real
  element IDs for sections or figures the tutor may need to focus with
  `open_file`. A tutor-tracked question uses `<div class="q" id="q1">`.
- A choice needing a tutor reply calls `SQ.choose(text, button)`. A
  tutor-reviewed answer calls `SQ.answer(qid, value, button)`. Local game
  controls can use JavaScript and give immediate feedback. `SQ.saveGame(key,
  data)` and `SQ.loadGame(key, callback)` save progress for that activity.
- Put formal test solutions in the parent-only answer key, outside the child's
  activity folder. For an unanswered paper question, `.answer-space` is a blank;
  when recording her answer, replace it with a neutral `.answered-note`.
- The app supplies the viewer's print control. Links between sibling HTML files
  do not work in the viewer; use same-page anchors or separate activity items.

## Real pictures

`find_image` saves an image beside the page and returns its filename and
attribution. Use that exact relative filename in `<img src="...">`, give it
useful alt text, and show the attribution beneath it. Generated images use
relative paths and alt text too. Do not embed base64 images or remote URLs.
For precise geometry or graphs, see `diagrams.md`.

## Check a figure before you finish

If the page includes a figure or chart, open it in the app and check that the
labels, values and shapes render correctly. The server supplies JSXGraph when
viewed in the app or through
`http://127.0.0.1:8010/api/workspace/raw?path=<encoded workspace path>`;
a bare `file://` page does not load that library. If the preview service is
unavailable, finish the page and say the figure was not visually checked.

## Answer widgets

A question answered on the page can send her answer to the tutor:

```html
<div class="q" id="q3">
  <p>Which is largest?</p>
  <button onclick="SQ.answer('q3','2/3',this)">2/3</button>
  <button onclick="SQ.answer('q3','3/5',this)">3/5</button>
</div>
```

For a typed answer, pass the input's value to `SQ.answer`. It ignores empty
answers and disables the submitting button to prevent duplicate turns. A game
that grades locally can handle its own controls instead.

## Timers

The app owns timed-test expiry; page JavaScript can display the countdown. Call
`SQ.startTimers([{qid:'q3',seconds:120}])` on load for a question, or use an
empty `qid` for a whole-test clock. Reopening the page does not restart an
existing app timer. `SQ.cancelTimer('q3')` stops a timer explicitly. On expiry
the page receives `{__sq:1, op:'timer-fired', qid:'q3'}` by `message`; disable
its answer controls then. The goal determines what the tutor does at expiry.
