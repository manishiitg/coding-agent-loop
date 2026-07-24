---
name: discover-something-new
description: A fun, off-syllabus "something new to explore" activity for the child — animated and engaging (interaction happens in chat, not on the page), tailored to grade and interests learned over time. Parent-initiated, handed off like any other activity.
---

# Discover something new

Trigger: the PARENT asks for something fun/off-syllabus for the child — "make her
something fun this weekend", "surprise her with something new and interesting",
"something new to explore" — NOT regular homework/study material tied to the
syllabus. This is curiosity content, not graded, and not something the child asks
for herself — you're building it for the parent to hand over, exactly like a test
or study guide.

1. **Know the child.** Read `memory/child-profile.json` for grade/age.

2. **Read what's known about her interests**, if it exists:
   `cat memory/interests.md`. This file is maintained automatically by a periodic
   check (never by you, mid-conversation) from her own conversations — read-only
   context here, same contract as `memory/preferences.md`.

3. **Pick a genuinely fun, age-appropriate topic.** If `memory/interests.md` shows
   a clear theme she's responded well to (e.g. loved space, animals, how things
   work), pick something ADJACENT and new within that theme — not a repeat. Check
   `ls Discoveries/` for topics already covered recently so you don't repeat one.
   With no interest history yet, pick something broadly delightful and a little
   surprising for her age: a weird true animal fact, a space mystery, how an
   everyday thing actually works, a strange bit of history — variety is fine
   early on.

4. **Create the activity folder** `Discoveries/<Topic>/<yyyy-mm-dd>-<slug>/` (date-stamp
   with `date -u +%Y-%m-%d`; `<Topic>` is a short theme name like "Space" or
   "Animals", or "General" if it doesn't fit one).

5. **Build ONE short, fun, animated COVER page** — read `skills/_shared/html-design.md`
   first (animation rules, and the SQ.choose pattern for any real choice). This
   is the title card the parent sees and hands over — it does NOT need to
   carry the whole discovery:
   - A fun title + one enticing opening line/teaser for the topic.
   - Use animation/hover generously — this page's whole job is to feel alive
     and fun, more so than ordinary study material.
   - A short, warm closing line inviting curiosity ("Ready to find out?").
   Write it into the activity folder you created. The actual fact-by-fact
   discovery — the guesses, the reveals, the "what should we explore next"
   choices — happens turn by turn once handed off, via `show_scene` (see step
   after next): that's what can use real SQ.choose buttons and follow
   wherever the child's own curiosity takes it, which a page fixed at creation
   time can't.

6. **Finalize and hand it off exactly like any other content** — call
   `create_learning_activity` with the folder as `dir`, a short fun `title` (e.g.
   "Something New!"), `items` = the page you wrote, `teaching_mode: "beginner"`
   (there's nothing to test or hint at here), and a `goal` — what actually
   finishing this discovery looks like (e.g. "get through all the facts, hear
   her guess for each one first, and end on the closing question"). The
   conversation WILL wander into her own tangents (that's good — engage with
   them), but `goal` is what you keep steering back toward so the discovery
   actually wraps up instead of drifting indefinitely. Then call
   `open_activity(dir)` so the parent sees it immediately on the right, per
   your main instructions. Do NOT call `open_file` directly or imply it's
   already on her screen — the parent taps "Give to `<child>`" when ready, same
   as a test or study guide.

7. **Tell the parent** in plain, warm words what you made and why (e.g. tying it
   to something she's shown interest in) — never mention `memory/interests.md`,
   file paths, or that you're "tracking" anything.

Never grade or score this, never treat it like a lesson. How well it lands is
learned automatically afterward from her own conversations — nothing you do here
updates `memory/interests.md` directly.

## Once handed off — the actual discovery happens via show_scene

The cover page from step 5 is just the title card. Once the child is handed
off and starts chatting, deliver each fact/beat as its own `show_scene` —
generated fresh, so it can follow HER reactions and tangents rather than a
fixed script:
- Each fact/beat: a small scene with the surprise, then (where it fits) an
  SQ.choose button offering a real next choice — "what should we explore
  next?", "which one do you think it is?" — never a `<details>` reveal.
- If she takes it somewhere the original cover page never anticipated
  (invents her own angle on the topic, asks to go a different direction),
  follow her there — a fresh scene matching wherever she's actually taking it
  beats forcing her back to a pre-planned sequence.
- Keep `goal` as the loose anchor (see main instructions) — engage with
  tangents, but steer back toward actually reaching some kind of closing
  moment rather than drifting forever.
