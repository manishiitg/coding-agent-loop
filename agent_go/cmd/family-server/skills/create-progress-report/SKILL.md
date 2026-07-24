---
name: create-progress-report
description: Build a designed, self-contained HTML progress report for the child — how she's actually doing, and what to work on next — surfaced in the Progress tab for both parent and child.
---

# Create a progress report

One self-contained HTML file that both parent and child can read: an honest picture
of **how she's doing** and a clear answer to **what to work on next**. A snapshot, not
a narrative essay — lead with the concrete and recent, then stop. If in doubt, cut a
section rather than pad it.

1. **Gather real evidence — the substance, not just filenames.**
   - Every activity's own `conversation.json` (e.g.
     `find . -maxdepth 4 -name conversation.json`, skipping `materials/`, `reports/`,
     `memory/`, `conversations/`). Read what actually happened: which problems came
     up, whether she got them on her own or needed hints, how many attempts, what
     tripped her up, any celebrate moments and why. This is the real signal — a list
     of activity titles and dates is not enough.
   - Each activity's `attempts/*.json` — what she's actually completed, and what the
     attempt shows.
   - The `<Subject>/<Topic>/` activity folders and `materials/` — what exists, so you
     can name what's covered versus not started.
   - For the **Overall** section you need the FULL history, not just the recent few:
     skim across the whole workspace, since that section is cumulative.
   - Never invent a score, a percentage, or a diagnosis that isn't directly backed by
     something you read.

2. **Write** it to `reports/progress.html` — always overwrite in place. This is one
   living document, fully regenerated each time; never append a dated entry or keep
   old wording "for history". Note the "as of" date inside the content
   (`date -u +%Y-%m-%d`). Style per `skills/_shared/html-design.md`.

   Cover these, in this order — keep the whole thing to about one screen:
   - **Right now** — the subject/topic of her most recent real activity and the last
     concrete thing she did, with the actual outcome. One or two lines; this is the
     headline, not a preamble.
   - **How she's doing** — an honest read of where she genuinely stands: what she can
     now do confidently on her own, where she still needs hints or stalls, and
     whether that's moving in the right direction over time. Ground every claim in
     something specific you actually read ("solved the a=1 case unaided twice, still
     stalls when a≠1"), never a generic compliment. If the evidence is thin, say so
     plainly in one line rather than inflating it.
   - **What to work on next** — the most useful next thing and *why it follows from
     the evidence above*, then one or two smaller supporting steps. Concrete enough
     to act on today ("ten minutes of mixed-denominator word problems, since that's
     where the last three misses clustered"), never "keep practising fractions".
     Where a real technique fits the pattern you're seeing (retrieval practice,
     spaced review, worked-example fading), name it and say plainly what it means —
     the parent may not know the term, and this is where they get real value.
   - **Overall** — a few compact cumulative facts from the FULL history: topics
     attempted, tests completed, star total and trend, one durable strength and one
     durable growth area that keep recurring across attempts. Facts and numbers only,
     never a topic-by-topic breakdown — that detail belongs in the Academic Map.

   Skip anything you'd only be padding with — no "recent activity" list restating
   what's already above, no closing note that adds nothing new.

3. **Tell the parent** it's ready and appears in the **Progress** tab, visible to
   both them and the child. Say the one next step in plain words too, so the value
   lands even if they don't open it.
