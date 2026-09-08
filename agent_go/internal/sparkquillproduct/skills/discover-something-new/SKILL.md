---
name: discover-something-new
description: A fun, off-syllabus "something new to explore" activity for the child — a light cover page plus a chat-driven discovery, tailored to her grade. Parent-initiated, handed off like any other activity.
---

# Discover something new

Trigger: the PARENT asks for something fun and off-syllabus — "make her something
fun this weekend", "surprise her with something new". This is curiosity content, not
graded, not tied to the syllabus, and not something the child requests herself.

1. **Know her.** `memory/child-profile.json` for grade/age.

2. **Pick something genuinely fun and age-appropriate.** Check `ls Discoveries/` so
   you don't cover a topic she's just had, and pick something broadly delightful and
   a little surprising for her age — a weird true animal fact, a space mystery, how
   an everyday thing actually works.

3. **Build a short, fun, animated COVER page** in
   `Discoveries/<Topic>/<yyyy-mm-dd>-<slug>/` (`<Topic>` is a short theme name like
   "Space" or "Animals", or "General"). This is only the title card the parent hands
   over — a fun title, one enticing teaser line, and a warm invitation ("Ready to
   find out?"). Lean on animation and hover more than ordinary study material: this
   page's whole job is to feel alive. It does NOT need to carry the discovery itself.

4. **Finalize** the activity with a `goal` describing what finishing actually looks
   like, and saying plainly that nothing here is being tested — e.g. "get
   through all the facts, hearing her guess for each one first, and end on the
   closing question".

5. **Tell the parent** what you made and why, warmly — ideally tying it to something
   she's shown interest in, without mentioning that anything is being tracked.

Never grade or score this.

## The discovery itself happens in chat, via show_scene

The cover page is just the opening. Once she starts, deliver each fact or beat as its
own `show_scene`, generated fresh so it can follow HER reactions rather than a fixed
script.

**Every scene that asks her something carries its answers as `SQ.choose` buttons** —
two to four of them ("which do you think it is?", "what should we explore next?").
This is the difference between playing and doing homework, and it is the single
thing most worth getting right here. Measured on a real space-detective session: ten
scenes ran with no button on any of them, so all thirteen of her replies had to be
typed out — by the end they had decayed into fragments and she asked to stop. She can
always type instead; the buttons exist so she doesn't have to.

Vary the rhythm too. In that same session every reply ended with another question,
and it turned a mystery into an interrogation. Sometimes just land the astonishing
thing and let it sit.

If she takes it somewhere the cover page never anticipated, follow her there; a fresh
scene matching where she's actually going beats forcing her back to a plan. Keep
`goal` as the loose anchor — engage with tangents, but steer toward an actual closing
moment rather than drifting forever.

6. **Then end the turn with `suggest_actions`** — "Tell the parent" above is not the last step.
