## Delegation & Sub-Agents (multi-agent chat)

In multi-agent chat you are an orchestrator: decompose work and hand pieces to sub-agents. This doc is the contract for how delegation works. (Inside a *workflow* the sub-agent surface is different — `call_sub_agent` / `call_generic_agent` on a todo_task step; this doc is about chat-mode `delegate`.)

### The four delegation tools

- **`delegate`** — spawn a sub-agent to do a self-contained task.
- **`query_agent`** — check a running/finished agent's status, history, and result (paginated via `last`/`offset`).
- **`terminate_agent`** — cancel a running agent.
- **`list_agents`** — list all background agents in this session.

### `delegate` parameters

- **`name`** (required) — short descriptive name for the agent.
- **`instruction`** (required) — comprehensive, **self-contained** instructions. The sub-agent does not see your conversation; give it everything (inputs, file paths, what "done" looks like, where to write output).
- **`reasoning_level`** (required) — `high` | `medium` | `low` (plus any custom tiers configured for the workspace). Maps to a provider/model. Use `high` for ambiguous/judgment work, `low` for mechanical work.
- **`agent_template`** (optional) — a template folder name from `subagents/` (see below).
- **`servers`** (optional) — MCP server names to scope the sub-agent's tools.
- **`skills`** (optional) — explicit skill folder names. **Sub-agents start with NO skills.** See pass-through below.
- **`share_browser`** (optional, default `true`) — share the browser session or isolate it.

### Execution model: async background

In multi-agent chat, `delegate` runs **asynchronously**. It returns immediately with an `agent_id` and `status: "running"`; the sub-agent runs in the background and you are notified when it completes. Do not block waiting — continue orchestrating other work, then use `query_agent(agent_id)` to collect the result (or wait for the completion notification). (If no background executor is wired — non-chat callers — it falls back to synchronous and returns the full result inline.)

### Depth limit: max 3 levels

Delegation depth is capped at **3** (`MaxDelegationDepth = 3`). The hierarchy is:

- **depth 0** — you, the root orchestrator
- **depth 1** — a child you spawn
- **depth 2** — a grandchild the child spawns
- a grandchild (depth 2) **cannot** delegate further — it gets `maximum delegation depth (3) reached`.

So plan for at most root → child → grandchild. Don't design flows that need deeper nesting; flatten them, or have the root fan out directly.

### Skills do NOT inherit — explicit pass only

A sub-agent starts with **zero** skills. It does **not** inherit the skills selected for your session. If the sub-agent needs a skill, name it in `delegate(skills=["folder-a", "folder-b"])`. This is deliberate ("explicit-pass" semantics) — it keeps each sub-agent's prompt scoped to exactly what its task needs. The same is true for MCP `servers`: pass the ones the sub-agent needs.

### Sub-agent templates (`subagents/`)

Reusable agent profiles live at `subagents/custom/<name>/SUBAGENT.md` (authored via the `subagent-creator` skill). Pass one with `delegate(agent_template="<name>")`.

**What a template actually applies at runtime** (important — do not over-rely on frontmatter):

- **`name`, `description`** — discovery/display metadata only.
- **`default_reasoning_level`** — applied **only if** you omit `reasoning_level` on the `delegate` call.
- **The Markdown body** — appended to the sub-agent's system prompt as its role.
- **`skills` / `servers` in frontmatter are NOT applied.** Even if a template lists them, the sub-agent will not get them. You must pass `skills=[...]` / `servers=[...]` explicitly on every `delegate` call that needs them.

So a template gives the sub-agent a *role and (optionally) a default reasoning tier* — not its tools or skills. Wire those per call.

### Orchestration discipline

- Write **self-contained** instructions — the sub-agent has none of your context.
- Tell it **where to write output** (a file path) so you can read the result deterministically rather than parsing prose.
- Spawn independent work in parallel (multiple `delegate` calls), then `query_agent` each — don't serialize what can run concurrently.
- For long chains, remember the depth-3 ceiling: prefer a wide root fan-out over deep nesting.
