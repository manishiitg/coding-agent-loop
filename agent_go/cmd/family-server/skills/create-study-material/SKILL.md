---
name: create-study-material
description: Create clear, child-ready study material (notes, worked examples, a revision sheet) for a topic, matched to the child's level and their own uploaded materials, packaged as one activity.
---

# Create study material

1. **Know the child and the material.** `memory/child-profile.json` for name, grade,
   and board (ask the parent if any is missing). The relevant
   `materials/<subject>/<topic>/*.meta.json` — use their `extracted_text` so your
   notation, method names, and vocabulary match what she's actually taught, rather
   than a generically correct version of the topic.

2. **Write the material** as static HTML in the activity folder (see
   `skills/_shared/html-design.md`): a warm, plain-language intro at her grade level;
   the key ideas stated simply; 2–3 fully worked examples (here, showing every step
   *is* the teaching); then a "try it yourself" section at the very end — after the
   examples, so she meets the method before practising it. Close with a line of
   encouragement addressed to her.

   Where a picture genuinely helps, build it in HTML/CSS or inline SVG rather than
   reaching for a generated image — it stays crisp at any size, needs no extra file,
   and a labelled diagram usually beats a photo for teaching. Don't force one in
   where it adds nothing.

3. **Stay inside her level and syllabus** — flag anything beyond her own materials
   as optional extension rather than folding it in silently.

4. **Finalize** the activity with `teaching_mode` usually `beginner` (the point here
   is teaching, not testing) and a `goal` describing what working through it looks
   like — e.g. "read every section and try the practice questions at the end".

5. **Tell the parent** what you made.
