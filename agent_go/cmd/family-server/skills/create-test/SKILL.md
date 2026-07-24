---
name: create-test
description: Create a practice test for the child from their materials and progress — child-facing questions with a separate parent-only answer key.
---

# Create a practice test

0. **Don't silently guess what to test.** If the parent's request already names a subject/topic/focus (e.g. "a quick check on fractions" or a quick-action button that names one), skip ahead. If it's generic ("make my child a test", "create a practice test") — before writing anything, quickly skim `parent/conversations/`, `child/conversations/`, and any recent `shared/tests/` for what the child has actually struggled with or last got wrong, then say what you found and ask ONE short question — e.g. "The last quick check showed still-shaky word problems with mixed denominators — want me to focus there, or something else?" Wait for their answer before generating. This is a real check-in, not a rhetorical one-liner you answer yourself in the same reply.

1. **Know the child.** Read `parent/child-profile.json` for name, grade, and board so the test matches their level. If any of these are missing, ask the parent first.

2. **Gather context and progress.**
   - Read the relevant material in `shared/materials/<subject>/<topic>/` and its `.meta.json` files.
   - Look at what the child has struggled with: skim `parent/conversations/` and `child/conversations/` (and any `.meta.json`) for weak spots. Focus some questions there.

3. **Write the test (child-facing, NO answers)** as a self-contained **HTML** file to
   `shared/tests/<subject>/<topic>/<yyyy-mm-dd>-<name>.html`
   (date-stamp with `date -u +%Y-%m-%d`; never overwrite an older test):
   - **The output format is ALWAYS this static HTML — never plain text or Markdown, no matter what.** If the parent asks for a "quick", "short", or "small" test, that changes ONLY the question count, never the format: a 3-question quick check gets exactly the same self-contained HTML and shared design as a 10-question test — just fewer `.card` elements. Producing a plain `.md`/`.txt` file is always wrong, even for a one-question test.
     - **BAD** (never do this, even for "just a short one"): writing `shared/tests/topic.md` with `**Q1.** Solve ...` in Markdown.
     - **GOOD**: `shared/tests/<subject>/<topic>/<date>-quick-check.html` — a small but real page: header strip, 3 numbered `.card` questions with space for working, styled per `skills/_shared/html-design.md`.
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - LAYOUT (make it look like a test paper): a compact top strip with name · grade/board · subject · total marks · time; questions grouped into sections; each question in a numbered `.card` with the marks as a `.badge` and clear blank space (or a few printed lines) to work in.
   - A clear header: child name, grade/board, subject, topic.
   - As many questions as the request calls for — a quick check might be 3, a full practice set 8–10 — at the child's level, easy → harder, covering the methods in their materials; include at least one targeting a known weak spot.
   - **View-only, static.** No answer box, no auto-save, no JS state — per `skills/_shared/html-design.md`. The child works it on paper or tells Quill their answers in chat; that's how the attempt reaches `child/attempts/`.
   - No answers and no hints that give them away — never embed the answer key in this file.

4. **Write the answer key (parent-only)** as plain **Markdown** (not HTML — it's parent-only reference material, no need for the shared design system) to `parent/answer-keys/<yyyy-mm-dd>-<name>-KEY.md` (same date stamp):
   - Full worked solutions.
   - A note on which questions target which weakness, so the parent knows what to watch.

5. **Tell the parent** what you made, where, and why those questions — in plain words.

Never put answers in the `shared/` test file. Answer keys live only under `parent/`, which the child cannot access.
