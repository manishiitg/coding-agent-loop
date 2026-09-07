---
name: create-progress-report
description: Build or refresh the one living progress page for the family — what the child has, how she is doing, what to work on next, and how the parent can help — from real evidence only.
---

# The progress page

One self-contained HTML file, `reports/progress.html`, that both parent and child
can read. It answers three questions in this order: **what she has**, **how she
is doing**, and **what to do next**. A snapshot fully regenerated each time, never
appended to; the "as of" date goes inside the content. Style per
`skills/guides/html-design.md`. If in doubt, cut a section rather than pad it.

1. **Gather real evidence — the substance, not just filenames.**
   - `materials/` — every subject and topic the family has uploaded, with each
     file's `.meta.json` (subject, topic, type, summary).
   - Every activity's `activity.json` (what exists per topic), its conversation
     with the tutor (what actually happened: which problems came up, whether
     she got them alone or with hints, how many attempts, what tripped her up,
     celebrate moments and why) and `attempts/*.json` (what she completed). This
     is the real signal; a list of titles and dates is not enough. The
     conversation is the platform's log, found from the activity's
     `product.json` session id — `skills/recall-conversations/SKILL.md` says
     exactly how to find and read it (older activities keep a
     `legacy-conversation.json` inside the folder instead).
   - For "right now" and "what next" only recent work matters; skip `archive/`.
     For the cumulative facts read everything, `archive/` included, so lifetime
     totals never shrink because the parent put something away.
   - Never invent a score, a percentage, a pattern or a diagnosis that is not
     directly backed by something you read. If the evidence is thin, say so.

2. **Write the page**, in this order:
   - **Right now** — her most recent real activity and the last concrete thing
     she did, with the outcome. One or two lines; the headline.
   - **Subject by subject** — the heart of the page, mostly visual. One `.card`
     per subject that really exists, in a `.grid`. Inside it one row per topic:
     what exists for it (materials, study material, a test; "nothing yet" is
     fine), and how she is doing on it as a meter row with a real value as text
     ("2 of 8 attempted", "9.5/15 on the school paper", "not started") in the
     matching state (secure, needs work, not started). One warm sentence per
     subject saying what the bars mean in plain words.
   - **Strengths** and **Where she's stuck** — a couple of bullets each. Every
     bullet names the specific MOVE ("subtracting mixed numbers when borrowing
     is needed"), never the chapter, and is grounded in something you read. Note
     the direction of travel where you can see it.
   - **What to work on next** — the most useful next thing and why it follows
     from the bullets above, then one or two smaller steps. Concrete enough to
     act on tonight.
   - **Helping her learn better** — two to four points for the parent, each
     grounded in a pattern you observed (what she responds to, what trips her
     up, how many attempts something took) and the technique that fits that kind
     of difficulty: retrieval practice and spaced review for a method she does
     inconsistently, worked examples with support faded out for one she cannot
     start, interleaving for two ideas she confuses. Name it, then say what doing
     it looks like tonight. Look something up only when it adds something real.
   - **Overall** — a few compact cumulative facts from the full history: topics
     attempted, tests completed, star total and trend, one durable strength and
     one durable growth area. Close with one encouraging line to her by name.

3. **Tell the parent** it's ready in the **Progress** tab, visible to them and
   the child, and say the one next step in plain words so the value lands even
   if they don't open it.

Rebuild it whenever materials or activities change so it stays a living view.
