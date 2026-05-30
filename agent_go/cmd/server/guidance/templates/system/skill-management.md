## Skill Management — install skills and attach them to workflows

Skills are reusable instruction sets (a `SKILL.md` plus optional bundled files) injected into step agents at runtime. This doc covers the full lifecycle: find → install → attach to a workflow → restrict per-step → remove. To *author* a new skill from scratch, use the `skill-creator` skill — do not hand-write `SKILL.md` content.

### Where skills live

Skills live at the **workspace root**, `<workspace-root>/skills/<folder>/SKILL.md`, and are **shared across all workflows**. Do NOT create or reference skills inside a workflow folder (e.g. `Workflow/trading/skills/` does not exist). Custom/authored skills live under `<workspace-root>/skills/custom/<folder>/`.

### Lifecycle

1. **Find**
   - `list_skills` — installed skills in this workspace.
   - `search_skills(query)` — search the public registry.
2. **Install**
   - `install_skill(source)` — from the registry, source format `owner/repo@skill-name`.
   - `import_skill(github_url)` — from a GitHub folder URL.
   - Both download into `<workspace-root>/skills/<folder>/`. If a folder exists but has no `SKILL.md`, reinstall it with the same method it was originally installed with — **never write `SKILL.md` by hand**.
3. **Attach to the workflow** (preset — all steps inherit)
   - `update_workflow_config(add_skills=["folder-name"])`. Do NOT edit `workflow.json` manually.
4. **Restrict to specific steps** (override)
   - By default every step inherits all workflow-level skills.
   - `update_step_config(step_id, enabled_skills=["skill-a"])` makes that step use **only** the listed skills.
   - `enabled_skills=[]` (empty array) = that step gets **no** skills.
5. **Remove from the workflow**
   - `update_workflow_config(remove_skills=["folder-name"])`.
6. **Uninstall**
   - `uninstall_skill(folder_name)` — deletes the files from the workspace entirely.

Inspect at any point with `get_workflow_config` (the workflow's selected skills) and `list_skills` (everything installed).

### Attachment model: workflow preset vs per-step (important)

There is **no cascade**. A step's effective skills are resolved as:

- Step has `enabled_skills` set (non-empty or empty) → that list is authoritative for the step. Non-empty = exactly those skills; empty `[]` = none.
- Step has no `enabled_skills` → it inherits the workflow preset (`add_skills`).

So per-step selection is an explicit override, not an addition. To clear a per-step override and fall back to the preset, use `update_step_config(step_id, clear=["enabled_skills"])`.

**Shared know-how that every step should see** does not belong in a skill attached to all steps — it belongs in `learnings/_global/SKILL.md`, which is auto-attached at every step launch. The step's `global_skill_objective` tunes how that global skill is applied. Use real installed skills for *reusable domain capabilities*; use `learnings/_global` for *this workflow's accumulated knowledge*.

### When to use workflow-level vs per-step

- **Workflow-level (`add_skills`)** — a capability most/all steps need (e.g. a Google Sheets skill in a reporting workflow). Default here; simplest.
- **Per-step (`enabled_skills`)** — a skill only one or two steps need, or when a step is overloaded with irrelevant skills. Each attached skill costs prompt tokens and dilutes the agent's focus, so scope narrowly when a workflow accumulates many skills.

### Troubleshooting

- **Skill not taking effect on a step** → check whether that step has an `enabled_skills` override that excludes it (`get_workflow_config` / step config). A non-empty `enabled_skills` ignores the preset.
- **Folder exists but no `SKILL.md`** → reinstall via the original method (`install_skill` / `import_skill`); do not author the file manually.
- **Too many skills / bloated step prompts** → move workflow-level skills to per-step `enabled_skills` on just the steps that need them.
- **Need to author or improve a skill** → use the `skill-creator` skill; for sub-agent templates use `subagent-creator`. Neither installs/attaches — that's this doc's tools.
