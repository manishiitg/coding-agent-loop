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
- Use `skill-creator` when the user asks to create or update a custom skill.
  Prefer a focused reusable skill over adding product-specific instructions to
  the system prompt.
- Before removing a skill, identify the exact installed name and explain that
  `uninstall_skill` removes the account-level installation, not only this
  project's selection.
- Load and follow a selected skill when its description matches the task. A
  skill does not grant filesystem, secret, network, MCP, or tool permissions
  beyond the current user and project.
