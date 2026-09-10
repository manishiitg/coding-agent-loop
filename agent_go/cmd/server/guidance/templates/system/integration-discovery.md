## Connecting a third-party service — CLI first, then a skill, then MCP

When a user asks to use a service in a workflow ("I want to use ClickUp/Notion/Stripe here"), there are three separate avenues, and they are not equally good. Check them **in this order** and stop at the first one that actually fits — do not default to MCP just because it is the most familiar path.

1. **CLI (preferred).** A well-maintained official or community CLI, invoked through `execute_shell_command`, is more token-efficient than MCP: it avoids loading a large tool schema and verbose response envelopes into context, and the agent acts through concise, purpose-built commands instead of a persistent tool-call loop. Most SaaS tools with developer APIs have one (`gh` for GitHub, `stripe` for Stripe, `gog` for Google Workspace).
2. **A skill wrapping that CLI.** If a CLI exists, check whether a skill already teaches how to drive it well (auth, common commands, output parsing, gotchas) before improvising — `search_skills(query)` searches the public skill registry, `list_skills` shows what is already installed. Prefer installing/using that skill over inventing usage patterns from scratch each time; see the `skill-management` reference for the full install/attach lifecycle.
3. **MCP (fallback).** Use MCP when no usable CLI exists, or when the integration genuinely needs MCP's strengths — persistent state, rich structured introspection, or a long-running agentic loop over a service's data model — rather than a batch of one-shot commands.

There is no registry of "which services have a CLI" to look up — figure it out the way a person would: try `execute_shell_command` (`which <name>`, `npm view <package>`, a quick web search) and reason from what you already know about the service. This is deliberately not automated; do not invent a fake curated list.

### Checking each avenue

- **CLI**: `execute_shell_command` — check if it's already on PATH (`which <cli>`), or search for the official package (npm/pip/brew/go install) and install it if the sandbox allows outbound network (it does by default; nothing needs to install it in advance for you). Read its `--help` before using it in a real step.
- **Skill**: `search_skills(query)` (public registry) and `list_skills` (already installed). Install with `install_skill(source)` or `import_skill(github_url)` — see `skill-management` for the rest of that lifecycle.
- **MCP**: `search_mcp_catalog(query)`. This checks three sources in one call:
  - **Our own catalog** — every entry here was hand-verified (correct OAuth shape, live-probed). A hit here is safe to connect from the connector directory (or, for a no-auth server, already usable) without further scrutiny.
  - **GitHub's public MCP Registry** and **Smithery's directory** — much larger, but **unvetted**. A hit here is a lead, not a recommendation: tell the user what you found (name, description, whether it's remote/HTTP or needs a local package run) and let them decide, the same way a human would evaluate any third-party integration before installing it. For a Smithery hit, `inspect_mcp_server(qualified_name)` shows its actual tool list before you or the user commit to anything. Once they say yes, use `install_mcp_server(name, url=...)` — it live-probes the URL's real auth requirements (no sign-in / API key / OAuth with DCR) rather than guessing, and is the right tool for any fresh URL, not `add_mcp_server` (that one is for a wholly custom server whose config you already know — no auth, no probing).

### Reporting back to the user

State plainly which avenue you're recommending and why (e.g. "ClickUp has an official CLI, so I'd rather use that than the ClickUp MCP — it'll be faster and use less context"). If more than one avenue exists, say so and let the user choose. If none exist, say that clearly rather than fabricating a plausible-sounding option.

### Related

- `skill-management` — the full skill find/install/attach/remove lifecycle once you've decided a skill is the right avenue.
- `workflow-tools` (Variables & Config section) — how to register (`add_mcp_server`) and select (`update_workflow_config add_servers`) an MCP server for a specific workflow once you've decided MCP is the right avenue.
