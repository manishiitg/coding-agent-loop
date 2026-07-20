---
name: create-test
description: Create a practice test for the child from their materials and progress — child-facing questions with a separate parent-only answer key.
---

# Create a practice test

1. **Know the child.** Read `parent/child-profile.json` for name, grade, and board so the test matches their level. If any of these are missing, ask the parent first.

2. **Gather context and progress.**
   - Read the relevant material in `shared/materials/<subject>/<topic>/` and its `.meta.json` files.
   - Look at what the child has struggled with: skim `parent/conversations/` and `child/conversations/` (and any `.meta.json`) for weak spots. Focus some questions there.

3. **Write the test (child-facing, NO answers)** to `shared/tests/<subject>/<topic>/<yyyy-mm-dd>-<name>.md`
   (date-stamp the filename with `date -u +%Y-%m-%d` so tests are kept date-wise and history is preserved — never overwrite an older test):
   - A short header: child name, grade/board, subject, topic.
   - 5–10 questions at the child's level, ordered easy → harder, covering the methods in their materials.
   - Include at least one question targeting a known weak spot.
   - No answers and no hints that give it away.

4. **Write the answer key (parent-only)** to `parent/answer-keys/<yyyy-mm-dd>-<name>-KEY.md` (same date stamp as the test):
   - Full worked solutions.
   - A note on which questions target which weakness, so the parent knows what to watch.

5. **Tell the parent** what you made, where, and why those questions — in plain words.

Never put answers in the `shared/` test file. Answer keys live only under `parent/`, which the child cannot access.
