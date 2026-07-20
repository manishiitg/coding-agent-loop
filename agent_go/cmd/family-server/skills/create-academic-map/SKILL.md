---
name: create-academic-map
description: Build a designed, self-contained HTML academic map of the child's subjects, topics, and materials that both parent and child can open.
---

# Create the academic map

Produce ONE self-contained HTML file giving a living overview of what the child is learning.

1. **Gather evidence — only what is really in the workspace, never invent subjects:**
   - List `shared/materials/` — every subject and its topics.
   - Read the `.meta.json` files for each material (subject, topic, type, summary).
   - Note generated work: `shared/study/` and `shared/tests/` (which topics have study material / tests).
   - Check for real attempt evidence per topic: read `child/attempts/*.json` (the child's saved answers) and skim `parent/conversations/` + `child/conversations/` for anything you observed about that topic (e.g. "solved the a=1 case, stuck on a≠1"). Use this for a short, honest status per topic — never a numeric score, never invented.
   - The current focus is in `parent/child-profile.json` and the active subject/topic.
   - Check `parent/preferences.md` for anything worth reflecting (e.g. a pacing or style note relevant to a subject).

2. **Write** the map to `shared/academic-map.html` (overwrite the existing placeholder). It MUST be:
   - Styled with the SHARED design system: read `skills/_shared/html-design.md` and inline its CSS + base template, so it matches every other generated HTML file.
   - Organised as: one card per **subject**, each listing its **topics**; for every topic show how many source materials, whether study material exists, whether a test exists, and a one-line evidence-based status (e.g. "Attempted 2026-07-20 — comfortable with a=1, stalls when a≠1", or "Not attempted yet" if there is no real evidence). Badge the **current** subject/topic.
   - Honest: if a subject has only uploads and no generated work yet, show that plainly. If the map is nearly empty, say it is just starting. Never show a percentage, grade, or score that wasn't actually computed from real graded work.

3. **Tell the parent** the map is updated and where it appears (the workspace / left menu).

Rebuild this whenever materials change so the map stays a living view — never hand-write topics that have no real materials behind them.
