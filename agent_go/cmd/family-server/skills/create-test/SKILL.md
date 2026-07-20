---
name: create-test
description: Create a practice test for the child from their materials and progress — child-facing questions with a separate parent-only answer key.
---

# Create a practice test

1. **Know the child.** Read `parent/child-profile.json` for name, grade, and board so the test matches their level. If any of these are missing, ask the parent first.

2. **Gather context and progress.**
   - Read the relevant material in `shared/materials/<subject>/<topic>/` and its `.meta.json` files.
   - Look at what the child has struggled with: skim `parent/conversations/` and `child/conversations/` (and any `.meta.json`) for weak spots. Focus some questions there.

3. **Write the test (child-facing, NO answers)** as a self-contained **HTML** file to
   `shared/tests/<subject>/<topic>/<yyyy-mm-dd>-<name>.html`
   (date-stamp with `date -u +%Y-%m-%d`; never overwrite an older test):
   - **The output format is ALWAYS this interactive HTML — never plain text or Markdown, no matter what.** If the parent asks for a "quick", "short", or "small" test, that changes ONLY the question count, never the format: a 3-question quick check gets exactly the same self-contained HTML, shared design, and SQ.save/load as a 10-question test — just fewer `.card` elements. Producing a plain `.md`/`.txt` file is always wrong, even for a one-question test.
     - **BAD** (never do this, even for "just a short one"): writing `shared/tests/topic.md` with `**Q1.** Solve ...` in Markdown.
     - **GOOD**: `shared/tests/<subject>/<topic>/<date>-quick-check.html` — a small but real interactive page: header strip, 3 `.card` questions with textareas, the SQ helper wired up, styled per `skills/_shared/html-design.md`.
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - LAYOUT (make it look like a test paper): a compact top strip with name · grade/board · subject · total marks · time; questions grouped into sections; each question in a numbered `.card` with the marks as a `.badge` and a labelled `<textarea>`/`<input>` for the answer; a small sticky "answered X/N" counter.
   - A clear header: child name, grade/board, subject, topic.
   - As many questions as the request calls for — a quick check might be 3, a full practice set 8–10 — at the child's level, easy → harder, covering the methods in their materials; include at least one targeting a known weak spot.
   - Usable on screen: each question in a card with a text box for the child to type their answer.
   - RECORD the child's answers: include the SQ helper from `skills/_shared/html-design.md` and call `SQ.save(key, answers)` whenever the child types, and `SQ.load(key)` on page load to restore — use the test's filename as the stable key. This saves the child's answers so you can mark them and give feedback later.
   - No answers and no hints that give them away — never embed the answer key in this file.

4. **Write the answer key (parent-only)** as **HTML** to `parent/answer-keys/<yyyy-mm-dd>-<name>-KEY.html` (same date stamp, same shared design):
   - Full worked solutions.
   - A note on which questions target which weakness, so the parent knows what to watch.

5. **Tell the parent** what you made, where, and why those questions — in plain words.

Never put answers in the `shared/` test file. Answer keys live only under `parent/`, which the child cannot access.
