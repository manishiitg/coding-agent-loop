---
name: create-study-material
description: Create clear, child-ready study material (notes, worked examples, a revision sheet) for a topic, matched to the child's level and their own uploaded materials.
---

# Create study material

1. **Know the child.** Read `parent/child-profile.json` for name, grade, and board so the material matches their level and syllabus. If any of these are missing, ask the parent first. Also read `parent/preferences.md` and apply anything relevant (teaching style, what to avoid).

2. **Gather context.** Read the relevant files in `shared/materials/<subject>/<topic>/` and their `.meta.json` so the material matches what the child is actually studying — same syllabus, same notation, same method names.

3. **Write child-ready study material** as a self-contained **HTML** file to
   `shared/study/<subject>/<topic>/<yyyy-mm-dd>-<name>.html`
   (date-stamp with `date -u +%Y-%m-%d`; never overwrite older material):
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - LAYOUT (make it look like a study guide): a short intro `.card`; "Key ideas" as a small `.grid` of compact `.card`s; each worked example in its own `.card`; a closing "Try it yourself" `.card` with the practice questions and space to work.
   - A short, warm intro to the topic in plain language for the child's grade.
   - The key ideas and definitions, stated simply, in cards.
   - 2–3 fully worked examples, step by step — this is teaching material, so showing the steps *is* the point.
   - **View-only, static** — no reveal buttons, no JS, no input boxes that save anything, per `skills/_shared/html-design.md`. Put "try it yourself" questions in their own `.card` at the end, after the worked examples, so the child sees the method before practicing — the ordering does the teaching, not a hide/reveal toggle.
   - An encouraging closing line addressed to the child.
   - Where a picture would genuinely help (a diagram, a simple labelled drawing), use the `generate_image` tool to create one and save it under the same `shared/study/<subject>/<topic>/` folder, then reference it with an `<img>` tag in the page. Don't force an illustration where a diagram wouldn't actually add anything.

4. **Stay at the child's level and syllabus.** Do not introduce content beyond their materials without flagging it as optional/extension.

5. **Tell the parent** what you made and where. It appears under study material in the workspace, ready for the child.
