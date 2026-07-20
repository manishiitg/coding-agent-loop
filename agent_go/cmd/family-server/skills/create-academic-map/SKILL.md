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
   - The current focus is in `parent/child-profile.json` and the active subject/topic.

2. **Write** the map to `shared/academic-map.html` (overwrite the existing placeholder). It MUST be:
   - A complete standalone HTML document — inline `<style>`, NO external assets or scripts.
   - Styled in the SparkQuill look: warm off-white `#fbf7ef`, deep-navy text `#16223a`, sunlit-yellow `#f6b93b` accents, soft rounded cards, a clear title with the child's name.
   - Organised as: one card per **subject**, each listing its **topics**; for every topic show how many source materials, whether study material exists, and whether a test exists. Badge the **current** subject/topic.
   - Honest: if a subject has only uploads and no generated work yet, show that plainly. If the map is nearly empty, say it is just starting.

3. **Tell the parent** the map is updated and where it appears (the workspace / left menu).

Rebuild this whenever materials change so the map stays a living view — never hand-write topics that have no real materials behind them.
