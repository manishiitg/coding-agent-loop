---
name: create-progress-report
description: Build a designed, self-contained HTML progress report for the child that both parent and child can open from the left menu.
---

# Create a progress report

Produce ONE self-contained HTML file that both parent and child can read.

1. **Gather evidence — only real data, never invent scores or diagnoses:**
   - Child profile: `parent/child-profile.json`.
   - Materials covered: list `shared/materials/` and read the `.meta.json` files for subjects and topics.
   - Work created: `shared/study/` and `shared/tests/`.
   - Activity: `parent/conversations/` and `child/conversations/` (titles + dates).

2. **Write** the report to `shared/reports/<yyyy-mm-dd>-<subject-or-overall>.html`
   (get the date with `date -u +%Y-%m-%d`). It MUST be:
   - A complete standalone HTML document — inline `<style>`, NO external assets, scripts, or fonts.
   - Warm and encouraging, readable by the child too — no harsh judgements, no numeric scores unless they truly exist.
   - Styled in the SparkQuill look:
     - Warm off-white background `#fbf7ef`, deep-navy text `#16223a`, sunlit-yellow accents `#f6b93b`, soft rounded cards with gentle shadows, a clear title showing the child's name and the date.
   - Sections:
     1. **Focus right now** — the current subject/topic.
     2. **What's going well** — grounded in real evidence.
     3. **What to practise next** — one or two things, framed kindly.
     4. **Recent activity** — real sessions/materials with dates.
     5. **A note for <child>** — a short, encouraging message to the child.
   - Every claim ties back to real evidence (a material, a session). If data is thin, say so honestly ("It's still early — here's what we have so far").

3. **Tell the parent** it's ready and that it now appears in the left menu under **Reports**, visible to both them and the child.

Keep it to one or two screens — clear, visual, honest.
