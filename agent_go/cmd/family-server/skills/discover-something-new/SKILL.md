---
name: discover-something-new
description: A fun, off-syllabus "something new to explore" activity for the child — interactive and animated, tailored to grade and interests learned over time. Parent-initiated, handed off like any other package.
---

# Discover something new

Trigger: the PARENT asks for something fun/off-syllabus for the child — "make her
something fun this weekend", "surprise her with something new and interesting",
"something new to explore" — NOT regular homework/study material tied to the
syllabus. This is curiosity content, not graded, and not something the child asks
for herself — you're building it for the parent to hand over, exactly like a test
or study guide.

1. **Know the child.** Read `parent/child-profile.json` for grade/age.

2. **Read what's known about her interests**, if it exists:
   `cat child/interests.md`. This file is maintained automatically by a periodic
   check (never by you, mid-conversation) from her own conversations — read-only
   context here, same contract as `parent/preferences.md`.

3. **Pick a genuinely fun, age-appropriate topic.** If `child/interests.md` shows
   a clear theme she's responded well to (e.g. loved space, animals, how things
   work), pick something ADJACENT and new within that theme — not a repeat. Check
   `ls shared/discoveries/` for topics already covered recently so you don't
   repeat one. With no interest history yet, pick something broadly delightful
   and a little surprising for her age: a weird true animal fact, a space
   mystery, how an everyday thing actually works, a strange bit of history —
   variety is fine early on.

4. **Build ONE short, fun, INTERACTIVE, ANIMATED page** — read
   `skills/_shared/html-design.md` first and follow its interactivity/animation
   rules exactly (buttons, `<details>` reveals, CSS transitions — never a real
   form control, per that skill's rules). This should feel like a delightful
   discovery, not a lesson:
   - Lead with something surprising or a "guess before you peek" reveal
     (`<details>` hiding the fun-fact payoff, not an answer key).
   - 2–4 short, punchy facts or beats — not a wall of text.
   - Use animation/hover/click generously — this page's whole job is to feel
     alive and fun, more so than ordinary study material.
   - A short, warm closing line inviting curiosity ("Want to hear something
     even wilder about this?").
   Save it to `shared/discoveries/<yyyy-mm-dd>-<topic-slug>.html` (date-stamp with
   `date -u +%Y-%m-%d`) — same folder family as `shared/study/`, `shared/tests/`.

5. **Hand it off exactly like any other content** — call `create_learning_activity`
   with a short, fun title (e.g. "Something New!") and this file as its one item,
   then call `open_activity` on the manifest it returns so the parent sees it
   immediately on the right, per your main instructions. Do NOT call `open_file`
   directly or imply it's already on her screen — the parent taps "Give to
   <child>" when ready, same as a test or study guide.

6. **Tell the parent** in plain, warm words what you made and why (e.g. tying it
   to something she's shown interest in) — never mention `child/interests.md`,
   file paths, or that you're "tracking" anything.

Never grade or score this, never treat it like a lesson. How well it lands is
learned automatically afterward from her own conversations — nothing you do here
updates `child/interests.md` directly.
