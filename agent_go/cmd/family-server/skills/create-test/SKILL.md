---
name: create-test
description: Create a practice test for the child from their materials and progress — child-facing questions with a separate parent-only answer key, packaged as one activity.
---

# Create a practice test

1. **Know the child and the material.** `memory/child-profile.json` for name, grade,
   and board (ask the parent if any is missing). The relevant
   `materials/<subject>/<topic>/*.meta.json` for what she's actually being taught —
   their `extracted_text` already holds the full content. Skim recent activity
   `conversation.json` files for weak spots worth targeting.

2. **Write the test** as static HTML in the activity folder (see
   `skills/_shared/html-design.md`). It should read like a real test paper: a clear
   header with her name, grade/board, subject and topic, then numbered questions
   easy → harder, marks shown as a `.badge`, and genuine space to work under each.
   Cover the methods that appear in her own materials, and include at least one
   question aimed at a known weak spot. No answers, and no hints that give them away.

3. **Write the answer key** as plain Markdown at `<name>-KEY.md` in that SAME folder
   — full worked solutions, plus a note on which questions target which weakness so
   the parent knows what to watch for. Never list it in `items`: it stays out of the
   child's activity view entirely, and what the tutor may reveal from it during her
   session is governed solely by `teaching_mode`.

4. **Finalize** the activity with `goal` = "answer all N questions" (N = the real
   count). A test is usually `strict` (hints only, no reveal) or `graduated` — ask
   rather than assume if the parent hasn't said.

5. **Tell the parent** what you made and why those particular questions.
