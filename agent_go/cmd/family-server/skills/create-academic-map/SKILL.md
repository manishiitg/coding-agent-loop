---
name: create-academic-map
description: Build a designed, self-contained HTML academic map of the child's subjects, topics, and materials, plus real coaching notes for the parent on how to teach them better.
---

# Create the academic map

Produce ONE self-contained HTML file giving a living overview of what the child is
learning, AND how the parent can help them learn it better.

1. **Gather evidence — only what is really in the workspace, never invent subjects:**
   - List `materials/` — every subject and its topics.
   - Read the `.meta.json` files for each material (subject, topic, type, summary).
   - Note generated work: walk the top-level Subject folders (everything except
     `materials/`, `reports/`, `memory/`, `conversations/`, `inbox/`, `skills/`) for
     activity folders — each has an `activity.json` (which topic study material /
     tests exist for).
   - Check for real attempt evidence per topic: read each activity's own
     `attempts/*.json` (the child's saved answers) and skim its `conversation.json`
     (plus the parent's own `conversations/parent.json`) for anything you observed
     about that topic (e.g. "solved the a=1 case, stuck on a≠1"). Use this for a
     short, honest status per topic — never a numeric score, never invented.
   - **For the coaching section**: read activity `conversation.json` files for real
     patterns — what she responds well to (e.g. word problems, worked examples),
     what trips her up (e.g. abstract notation, a specific step), how many attempts
     something took, celebrate moments. This is the evidence for "how to teach her
     better" — never generic advice untethered to what actually happened.

2. **Write** the map to `reports/academic-map.html` (overwrite the existing placeholder — and overwrite it completely every time you rebuild it, not just append to it). It MUST be:
   - A pure **current-state snapshot** — this file has no history and never accumulates. It always describes the situation as of right now, fully regenerated from the evidence in step 1. Never add a dated entry, a "latest update" block, or an "earlier record" — there is no such thing here; the whole file is replaced with a fresh current picture every time (the Progress Report, not this file, is where "what's recently changed" belongs — see its own skill).
   - Styled with the SHARED design system: read `skills/_shared/html-design.md` and inline its CSS + base template, so it matches every other generated HTML file.
   - Organised as: one card per **subject**, each listing its **topics**; for every topic show how many source materials, whether study material exists, whether a test exists, and a one-line evidence-based status (e.g. "Attempted 2026-07-20 — comfortable with a=1, stalls when a≠1", or "Not attempted yet" if there is no real evidence). Do not badge or call out a "current" topic — this map is a plain inventory, not a status update.
   - **Then, a "Helping [child] learn better" section for the parent** — 2-4 short, concrete points, each grounded in something real you observed (not generic filler): a pattern in how she learns (e.g. "she gets through word problems fast but slows down on abstract notation — lead with a concrete example before the abstract form"), plus a specific technique to try next (retrieval practice, spaced review, worked-example fading, etc.) matched to that pattern. If genuinely useful, use web_search for a board-specific exam technique or a well-known teaching strategy that fits what you observed — but only when it adds something real, not as padding.
   - Honest: if a subject has only uploads and no generated work yet, show that plainly. If the map is nearly empty, say it is just starting. Never show a percentage, grade, or score that wasn't actually computed from real graded work. Never invent a learning pattern that isn't backed by something you actually read.

3. **Tell the parent** the map is updated and where it appears (the **Academics** tab).

Rebuild this whenever materials change so the map stays a living view — never hand-write topics that have no real materials behind them, and never hand-write a coaching point that isn't backed by something you actually observed.
