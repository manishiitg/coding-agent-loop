---
name: create-study-material
description: Create clear, child-ready study material (notes, worked examples, a revision sheet) for a topic, matched to the child's level and their own uploaded materials.
---

# Create study material

1. **Know the child.** Read `parent/child-profile.json` for name, grade, and board so the material matches their level and syllabus. If any of these are missing, ask the parent first.

2. **Gather context.** Read the relevant files in `shared/materials/<subject>/<topic>/` and their `.meta.json` so the material matches what the child is actually studying — same syllabus, same notation, same method names.

3. **Write child-ready study material** as a self-contained **HTML** file to
   `shared/study/<subject>/<topic>/<yyyy-mm-dd>-<name>.html`
   (date-stamp with `date -u +%Y-%m-%d`; never overwrite older material):
   - Style it with the SHARED design system — read `skills/_shared/html-design.md` and inline its CSS + base template.
   - A short, warm intro to the topic in plain language for the child's grade.
   - The key ideas and definitions, stated simply, in cards.
   - 2–3 fully worked examples, step by step — this is teaching material, so showing the steps *is* the point.
   - Make it **interactive** where it helps learning: put the "try it yourself" questions at the end and hide each worked solution behind a "Show me how" button the child clicks *after* trying (a small inline `<script>` toggling visibility is fine — keep the file self-contained).
   - An encouraging closing line addressed to the child.

4. **Stay at the child's level and syllabus.** Do not introduce content beyond their materials without flagging it as optional/extension.

5. **Tell the parent** what you made and where. It appears under study material in the workspace, ready for the child.
