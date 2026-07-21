---
name: create-progress-report
description: Build a designed, self-contained HTML progress report for the child, surfaced in the Progress tab for both parent and child.
---

# Create a progress report

Produce ONE self-contained HTML file that both parent and child can read. This is a
**snapshot of where things stand right now** — not a narrative essay. Be direct: lead
with the concrete, recent, specific stuff, then stop. If in doubt, cut a section rather
than pad it.

1. **Gather evidence — read the actual substance, not just filenames/titles:**
   - `child/conversations/*.json` — cat the most recent few and read what genuinely
     happened: which specific problems came up, whether she got them right on her own
     or needed hints, what tripped her up, any celebrate moments (stars + why). This is
     the real signal — a list of conversation titles/dates is NOT enough on its own.
   - `child/attempts/*.json` — which tests/practice she's actually done, and what the
     attempt shows.
   - `shared/tests/`, `shared/study/`, `shared/materials/` — what currently exists, so
     you can name what's covered vs. not started.
   - `parent/child-profile.json` — current star total.
   - Never invent a score, a percentage, or a diagnosis that isn't directly backed by
     one of the above.

2. **Write** the report to the single fixed path `shared/reports/progress.html`
   (overwrite it in place every time — this is one living document, not a new
   dated file each update; note the "as of" date inside the content itself, via
   `date -u +%Y-%m-%d`). Style it with the shared design system (read
   `skills/_shared/html-design.md`, inline its CSS + base template).

   Keep it SHORT and DIRECT — one screen, no scrolling essay:
   - **Right now**: 1-2 lines — whatever subject/topic the most recent real activity
     was on (there's no stored "current" field; infer it from the evidence you just
     read), and the single most recent concrete thing she did (a specific problem, a
     specific test), with the real outcome. This is the headline, not a preamble.
   - **Worth noting**: 2-3 short bullets max, each tied to one specific, real
     moment (a problem she solved after effort, a test she finished, a pattern you
     genuinely see across attempts) — not generic encouragement, not restating
     "Right now."
   - **Next**: one line, one concrete next step.
   - Skip anything you'd otherwise pad with — no separate "recent activity" list
     that just repeats what's already above, no generic closing note unless it says
     something the rest of the report hasn't already said.
   - If evidence is thin, say so in one honest line instead of filling space.

3. **Tell the parent** it's ready and that it now appears in the **Progress** tab,
   visible to both them and the child.
