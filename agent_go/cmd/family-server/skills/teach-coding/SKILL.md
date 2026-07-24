---
name: teach-coding
description: Age/grade-appropriate approach for a coding/programming subject — read this BEFORE create-study-material or create-test when the topic is coding/programming, in addition to that skill.
---

# Teaching coding

Coding is not like other subjects: the right material is very different by age, and
—especially before high school— the goal is **computational thinking** (breaking a
problem into steps, sequencing, loops, conditionals, spotting a logic error), not
memorizing a programming language. A child who can confidently trace through and
debug simple logic has learned the actually valuable skill, even if they've barely
touched real syntax. Don't over-index on language features or "cover the syntax" —
index on reasoning.

Read `parent/child-profile.json` for grade, then match ONE of these bands:

1. **Grade ~KG–2 (roughly ages 5–7): no real syntax on screen at all.** This age
   needs hands-on, drag-and-drop tools (ScratchJr, code.org's earliest courses) —
   a full coding tool cannot give them that here. Tell the parent this plainly
   rather than faking it with text code a 6-year-old can't use. What you CAN
   make: a CLICKABLE unplugged logic page (see the interactivity note below) —
   click arrows/buttons to move a character step by step to a treasure, or tap
   pictures into the right order — no code syntax anywhere, just sequencing as
   a game.

2. **Grade ~3–8 (roughly ages 8–13) — the default for most requests, logic FIRST,
   MADE REAL through something she can actually click and see work.** The point
   is computational thinking, with light syntax only as the vehicle to express
   it — NOT language mastery. A page she can only read is far less exciting than
   one she can interact with, so PREFER building a small, complete, ALREADY-WORKING
   interactive mini-experience that embodies the concept, over a dry written
   exercise:
   - **Conditionals** → a click-through choose-your-own-adventure (click "go
     left" / "go right", the story branches for real).
   - **Sequencing** → a step-by-step character/robot that moves one click at a
     time toward a goal, each click revealing the next step.
   - **Loops** → an on-screen counter or repeating animation she advances by
     clicking "repeat", visibly showing the same action happening N times.
   - **Variables/state** → something simple that visibly changes and remembers
     a value across clicks (a score, a health bar, an inventory count).
   Build these with plain buttons + JS show/hide (per the interactivity note
   below) — NOT a text input for guesses/answers; turn "guess a number" into
   "pick one of these 5 buttons" so it never needs a real form control.
   Alongside the working demo, show the short, simplified logic/pseudocode
   that drives it (a styled read-only block, e.g. "if left button clicked →
   show the left path") so she connects *this reasoning* to *that behavior* —
   the demo is a teaching tool for the logic, not a disconnected game.
   Use the drier styles below as reinforcement AFTER the demo, not instead of it:
   - "Predict the output" — a short snippet, ask what it prints/does, then explain why.
   - "Find the bug" — a snippet with one deliberate logic error to spot and explain.
   - "Trace it by hand" — walk through a loop or conditional step by step on paper.
   Introduce only a FEW real constructs at a time and spend most of the material
   on reasoning about them, not breadth of language features. This band is where
   most requests for "coding for kids" land.

3. **Grade ~9–12 (roughly ages 14+): real code, real small projects.** Syntax
   depth can grow and independent building matters more here, but problem-solving
   and finishing something real still beats exhaustively covering language
   features — favor small, complete projects (a simple game, a script that
   solves a real small task) over syntax tours. The same "build something that
   visibly works" instinct from band 2 still applies, just with more real syntax
   shown alongside it.

Format and the interactivity distinction (read `skills/_shared/html-design.md`
first): the "no form controls" rule there (no `<input>`/`<textarea>`/`<select>`)
still applies — build all clicking/branching with plain `<button>`s and JS
show/hide, never a text box. That rule is NOT the same as "no interactivity" —
buttons, branching content, `<details>` reveals, and CSS animation are all
explicitly welcome (see that skill's "visually engaging" section) and are
exactly what makes a coding demo feel real instead of a page to read. What
remains genuinely out of scope is a live code EDITOR where she types and runs
her OWN arbitrary code — this app has no code-execution engine; never build or
imply one. Every demo here is pre-built by you and already working when she
opens it, not something she programs herself.
