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
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - A clear header: child name, grade/board, subject, topic.
   - 5–10 questions at the child's level, easy → harder, covering the methods in their materials; include at least one targeting a known weak spot.
   - Usable on screen: each question in a card with a text box or lined space for the child to write their answer.
   - No answers and no hints that give them away. (A small inline `<script>` for niceties like a timer is fine, but never embed the answers.)

4. **Write the answer key (parent-only)** as **HTML** to `parent/answer-keys/<yyyy-mm-dd>-<name>-KEY.html` (same date stamp, same shared design):
   - Full worked solutions.
   - A note on which questions target which weakness, so the parent knows what to watch.

5. **Tell the parent** what you made, where, and why those questions — in plain words.

Never put answers in the `shared/` test file. Answer keys live only under `parent/`, which the child cannot access.
