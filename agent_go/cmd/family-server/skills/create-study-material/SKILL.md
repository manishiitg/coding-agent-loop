---
name: create-study-material
description: Create clear, child-ready study material (notes, worked examples, a revision sheet) for a topic, matched to the child's level and their own uploaded materials, packaged as one activity.
---

# Create study material

0. **Don't silently guess what to make it about.** If the parent's request already names a subject/topic/focus, skip ahead. If it's generic ("create study material for my child") — before writing anything, quickly skim recent activity conversations and the academic map for what the child is currently on or struggling with, then say what you found and ask ONE short question — e.g. "Currently on fractions and decimals, and seemed to struggle with word problems last time — want me to focus there, or a different topic?" Wait for their answer before generating.

1. **Run the interactive intake.** Before generating, ask (skipping anything the parent already told you) how the child should be handled when stuck for THIS material (`teaching_mode` — study material is usually `beginner`, since the point is teaching, not testing) and any tutor tone/persona preference.

2. **Know the child.** Read `memory/child-profile.json` for name, grade, and board so the material matches their level and syllabus. If any of these are missing, ask the parent first.

3. **Gather context.** Read the relevant `.meta.json` files in `materials/<subject>/<topic>/` — use their `extracted_text` (the full content process-file already extracted once) so the material matches what the child is actually studying — same syllabus, same notation, same method names. Only open the raw file itself if `extracted_text` is missing or insufficient.

4. **Create the activity folder** `<Subject>/<Topic>/<yyyy-mm-dd>-<slug>/` (date-stamp with `date -u +%Y-%m-%d`; never reuse an older activity's folder).

5. **Write child-ready study material** as a self-contained **HTML** file INTO that folder:
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - LAYOUT (make it look like a study guide): a short intro `.card`; "Key ideas" as a small `.grid` of compact `.card`s; each worked example in its own `.card`; a closing "Try it yourself" `.card` with the practice questions and space to work.
   - A short, warm intro to the topic in plain language for the child's grade.
   - The key ideas and definitions, stated simply, in cards.
   - 2–3 fully worked examples, step by step — this is teaching material, so showing the steps *is* the point.
   - **View-only, static** — no reveal buttons, no JS, no input boxes that save anything, per `skills/_shared/html-design.md`. Put "try it yourself" questions in their own `.card` at the end, after the worked examples, so the child sees the method before practicing — the ordering does the teaching, not a hide/reveal toggle.
   - An encouraging closing line addressed to the child.
   - Where a picture would genuinely help (a diagram, a simple labelled drawing), use the `generate_image` tool to create one and save it INSIDE the same activity folder, then reference it with an `<img>` tag in the page. Don't force an illustration where a diagram wouldn't actually add anything.

6. **Stay at the child's level and syllabus.** Do not introduce content beyond their materials without flagging it as optional/extension.

7. **Finalize the activity**: call `create_learning_activity` with the folder as `dir`, a short `title`, `items` = the file(s) you wrote, the `teaching_mode`/`persona` from the intake, and a `goal` describing what actually working through this material looks like (e.g. "read through every section and try the practice questions at the end"). Then call `open_activity(dir)` so the parent sees it on the right with its "Give to `<child>`" button.

8. **Tell the parent** what you made — in plain words, no paths or filenames.
