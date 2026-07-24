---
name: create-test
description: Create a practice test for the child from their materials and progress — child-facing questions with a separate parent-only answer key, packaged as one activity.
---

# Create a practice test

0. **Don't silently guess what to test.** If the parent's request already names a subject/topic/focus (e.g. "a quick check on fractions" or a quick-action button that names one), skip ahead. If it's generic ("make my child a test", "create a practice test") — before writing anything, quickly skim recent activity conversations and any recent tests for what the child has actually struggled with or last got wrong, then say what you found and ask ONE short question — e.g. "The last quick check showed still-shaky word problems with mixed denominators — want me to focus there, or something else?" Wait for their answer before generating. This is a real check-in, not a rhetorical one-liner you answer yourself in the same reply.

1. **Run the interactive intake.** Before generating, ask (skipping anything the parent already told you): how many questions, how the child should be handled when stuck for THIS test (`teaching_mode` — `strict` for a real assessment with hints only and no reveal, `graduated` for a few hints then the answer, `beginner` if this is really meant to teach rather than test), and any tutor tone/persona preference. A real test is usually `strict` or `graduated` by default — ask if unsure rather than assuming.

2. **Know the child.** Read `memory/child-profile.json` for name, grade, and board so the test matches their level. If any of these are missing, ask the parent first.

3. **Gather context and progress.**
   - Read the relevant material in `materials/<subject>/<topic>/` and its `.meta.json` files.
   - Look at what the child has struggled with: skim recent activity `conversation.json` files and the parent's own conversation for weak spots. Focus some questions there.

4. **Create the activity folder** `<Subject>/<Topic>/<yyyy-mm-dd>-<slug>/` (date-stamp with `date -u +%Y-%m-%d`; never reuse an older activity's folder) and write the test INTO it.

5. **Write the test (child-facing, NO answers)** as a self-contained **HTML** file at
   `<Subject>/<Topic>/<yyyy-mm-dd>-<slug>/<name>.html`:
   - **The output format is ALWAYS this static HTML — never plain text or Markdown, no matter what.** If the parent asks for a "quick", "short", or "small" test, that changes ONLY the question count, never the format: a 3-question quick check gets exactly the same self-contained HTML and shared design as a 10-question test — just fewer `.card` elements. Producing a plain `.md`/`.txt` file is always wrong, even for a one-question test.
     - **BAD** (never do this, even for "just a short one"): writing `topic.md` with `**Q1.** Solve ...` in Markdown.
     - **GOOD**: `<Subject>/<Topic>/<date>-quick-check/quick-check.html` — a small but real page: header strip, 3 numbered `.card` questions with space for working, styled per `skills/_shared/html-design.md`.
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - LAYOUT (make it look like a test paper): a compact top strip with name · grade/board · subject · total marks · time; questions grouped into sections; each question in a numbered `.card` with the marks as a `.badge` and clear blank space (or a few printed lines) to work in.
   - A clear header: child name, grade/board, subject, topic.
   - As many questions as the parent asked for (from the intake) at the child's level, easy → harder, covering the methods in their materials; include at least one targeting a known weak spot.
   - **View-only, static.** No answer box, no auto-save, no JS state — per `skills/_shared/html-design.md`. The child works it on paper or tells Quill their answers in chat; the tutor records progress notes directly onto this file as the child works through it.
   - No answers and no hints that give them away — never embed the answer key in this file.

6. **Write the answer key** as plain **Markdown** (not HTML — it's reference material, no need for the shared design system) INSIDE that SAME activity folder as `<name>-KEY.md` (same date stamp) — right alongside the test, never in a separate location:
   - Full worked solutions.
   - A note on which questions target which weakness, so the parent knows what to watch.
   - This file must NOT be listed as an item when you call `create_learning_activity` below — it stays out of `items` so the child never sees it in their activity view; what the tutor is allowed to reveal from it is governed entirely by `teaching_mode`.

7. **Finalize the activity**: call `create_learning_activity` with the folder as `dir`, a short `title`, `items` = just the test's bare filename (not the key), and the `teaching_mode`/`hints_before_answer`/`persona` from the intake. Then call `open_activity(dir)` so the parent sees it on the right with its "Give to `<child>`" button.

8. **Tell the parent** what you made and why those questions — in plain words, no paths or filenames.

Never put answers in the child-facing test file. The answer key lives in the same activity folder but is never part of `items`, so the child never sees it — and what the tutor reveals from it during the child's turn is governed entirely by that activity's `teaching_mode`.
