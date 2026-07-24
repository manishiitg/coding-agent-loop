---
name: teach-coding
description: Age/grade-appropriate approach for a coding/programming subject — read this BEFORE create-study-material or create-test when the topic is coding/programming, in addition to that skill.
---

# Teaching coding

Coding is not like other subjects: the right material differs sharply by age, and
— especially before high school — the goal is **computational thinking** (breaking a
problem into steps, sequencing, loops, conditionals, spotting a logic error), not
memorizing a language. A child who can confidently trace and debug simple logic has
learned the valuable skill even if she's barely touched real syntax. Index on
reasoning, not on covering language features.

Read `memory/child-profile.json` for grade, then match ONE band:

1. **Grade ~KG–2 (ages ~5–7): no syntax on screen at all.** This age needs
   hands-on, drag-and-drop tools (ScratchJr, code.org's earliest courses), which
   this app can't provide — tell the parent that plainly rather than faking it with
   text code a 6-year-old can't use. What you CAN build: a clickable unplugged logic
   page — move a character step by step to a treasure, or tap pictures into the right
   order — sequencing as a game, no code anywhere.

2. **Grade ~3–8 (ages ~8–13) — the default for most requests. Logic first, made
   real through something she can click and watch work.** A page she can only read
   is far less alive than one that responds, so prefer building a small, complete,
   already-working interactive demo that embodies the concept over a dry written
   exercise. Conditionals suit a branching choose-your-own-adventure; sequencing, a
   character that advances one step per click; loops, a counter or repeating
   animation she drives; variables, something that visibly changes and remembers a
   value across clicks.

   Alongside the demo, show the short simplified pseudocode that drives it (a styled
   read-only block, e.g. "if left button clicked → show the left path") so she
   connects *this reasoning* to *that behaviour* — the demo teaches the logic, it
   isn't a disconnected game. Then reinforce with the drier styles AFTER it, never
   instead of it: "predict the output", "find the bug" (one deliberate logic error),
   "trace it by hand". Introduce only a few constructs at a time.

3. **Grade ~9–12 (ages ~14+): real code, real small projects.** Syntax depth and
   independent building matter more here, but finishing something real still beats a
   syntax tour — favour small complete projects (a simple game, a script that solves
   an actual small task). The "build something that visibly works" instinct from band
   2 still applies, just with more real syntax alongside it.

**Interactivity** follows `skills/_shared/html-design.md` as-is: build clicking and
branching with plain `<button>`s (never a text input), and any button representing a
genuine CHOICE must use SQ.choose so you actually see what she picked. Purely
decorative motion — a counter ticking, a sprite moving — is fine as plain client-side
JS, since nothing is being chosen. A demo can also live in a `show_scene` snippet
rather than the activity's fixed file when you want it to follow a direction she
takes the lesson in.

Out of scope: a live code EDITOR where she types and runs her own arbitrary code —
this app has no code-execution engine, so never build or imply one. Every demo is
pre-built by you and already working when she opens it.
