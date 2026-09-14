---
name: work-skills
description: Discover, install, import, create, select, and remove reusable skills in Work. Use when the user asks to add a capability or manage the skills available to a project or account.
---

# Work skills

- Use `list_skills` to inspect what is already installed before searching or
  creating another skill. Use `search_skills` to discover an existing skill.
- Use `install_skill` for a discovered skill and `import_skill` for a supplied
  skill source. Installation is account-level; selecting it for this project is
  a separate action in **Setup > Skills**.
- When the user explicitly asks to preserve or improve a repeatable procedure,
  create or update a focused custom skill under
  `skills/custom/<skill-name>/SKILL.md`. Inspect existing custom skills first
  and update a matching skill instead of creating a duplicate. Keep the normal Work
  identity; skill authoring is a capability, not a separate chat persona.
- A custom skill needs concise YAML frontmatter with `name` and `description`,
  followed by only the non-obvious instructions that improve future work. Add
  scripts or references only when they provide concrete reusable value.
- Never store credentials or secret values in a skill. Document required secret
  names and use the Secrets system at runtime.
- Before removing a skill, identify the exact installed name and explain that
  `uninstall_skill` removes the account-level installation, not only this
  project's selection.
- Load and follow a selected skill when its description matches the task. A
  skill does not grant filesystem, secret, network, MCP, or tool permissions
  beyond the current user and project.
