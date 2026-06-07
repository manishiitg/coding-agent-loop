## LLM Provider Config - published chat models and provider auth

Published LLMs and provider credentials are managed by tools. Never inspect or edit `config/` files with shell or file tools.

### Which tool to call

- User asks "which LLMs do we have?" or "what is published?" -> call `list_published_llms`.
- User asks "what models can this provider publish?" -> call `list_provider_models(provider="...")`.
- User asks whether a candidate works -> call `test_llm(provider="...", model_id="...", options={...})`.
- User asks to publish or update an LLM -> call `test_llm` first when practical, then `save_published_llm`.
- User asks to add/change credentials -> call `set_provider_auth`; never paste keys into shell commands or config files.

### Minimal published LLM shape

Store only the routable model identity and model-specific options (the `name`/`model_id` below are an **example** — use a current model id from `list_llm_capabilities` / `list_provider_models`, not a hardcoded version):

```json
{
  "name": "Claude Opus 4.1 high",
  "provider": "claude-code",
  "model_id": "claude-opus-4-1",
  "options": {
    "reasoning_effort": "high"
  }
}
```

`id` and `created_at` may be assigned by the system. `options.reasoning_effort` is the field that preserves low/medium/high reasoning for providers such as Claude Code and Codex CLI. Do not invent or persist `context_window`, pricing fields, `auth_method`, `temperature`, or tool-call settings unless a dedicated tool explicitly asks for them.

### Correct publishing flow

1. Call `list_provider_models(provider="...")` and use the returned model id / supported reasoning levels.
2. Call `test_llm` with the exact provider, model id, and any `options.reasoning_effort` the user requested.
3. Call `save_published_llm` with `name`, `provider`, `model_id`, and optional `options`.
4. Call `list_published_llms` to verify the saved entry.

### Code-execution bridge pattern

When only `execute_shell_command` is directly exposed, call custom tools through `$MCP_CUSTOM`:

```bash
payload='{}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/list_published_llms"

payload='{"provider":"claude-code"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/list_provider_models"
```

Use the same pattern for `test_llm`, `save_published_llm`, and `set_provider_auth`.
