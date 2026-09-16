---
name: work-skills
description: Discover, install, import, create, select, and remove reusable skills in Crew. Use when the user asks to add a capability or manage the skills available to a project or account.
---

# Crew skills

- Use `list_skills` to inspect what is already installed before searching or
  creating another skill. Use `search_skills` to discover an existing skill.
- Use `install_skill` for a discovered skill and `import_skill` for a supplied
  skill source. Installation is account-level; selecting it for this project is
  a separate action in **Setup > Skills**.
- When the user explicitly asks to preserve or improve a repeatable procedure,
  create or update a focused custom skill under
  `skills/custom/<skill-name>/SKILL.md`. Inspect the names and descriptions of
  every existing custom skill before choosing a destination.
- Update an existing skill only when the new knowledge has the same topic and
  would be loaded for the same kind of future request. Create a separate,
  clearly named skill when the topic, external system, audience, trigger, or
  intended outcome differs. A single request may create or update several
  skills when it contains independently reusable topics.
- Do not use one custom skill as general project memory, append unrelated notes
  to the most recently edited skill, or combine topics merely because they were
  discussed in the same conversation. If an existing skill has already become
  a catch-all, split the relevant material into focused skills when updating it.
  Keep the normal Crew identity; skill authoring is a capability, not a
  separate chat persona.
- A custom skill needs concise YAML frontmatter with `name` and `description`,
  followed by only the non-obvious instructions that improve future work. Add
  scripts or references only when they provide concrete reusable value. Keep
  `SKILL.md` small: target 150 lines or fewer. Move detailed examples,
  background explanations, schemas, and lookup tables into focused files under
  `references/`; move repeatable executable logic into `scripts/`. If the core
  instructions still grow beyond that target, split the skill by topic or
  trigger instead of making the main file longer.
- Never store credentials or secret values in a skill. Document required secret
  names and use the Secrets system at runtime.
- Before removing a skill, identify the exact installed name and explain that
  `uninstall_skill` removes the account-level installation, not only this
  project's selection.
- Load and follow a selected skill when its description matches the task. A
  skill does not grant filesystem, secret, network, MCP, or tool permissions
  beyond the current user and project.
