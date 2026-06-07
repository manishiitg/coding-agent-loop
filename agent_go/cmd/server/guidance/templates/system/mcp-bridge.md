## MCP Bridge — HTTP API for Tool Calls

This skill covers the full mechanics of the MCP HTTP bridge: the env vars
that are pre-set, the curl pattern for calling MCP tools, filesystem
rules, and the workflow loop. The short version lives in the
`system-tools` skill; this is the deep reference.

The bridge is active whenever the session is in agentic or scripted mode
(code-execution infrastructure on). In direct tool-call mode the bridge
is not used — call tools as LLM functions instead.

## Tool access — HTTP, not direct

Through the bridge, MCP tools are reachable via authenticated HTTP, not
as direct LLM tool calls. The full set of available servers and their
tools is in the `<available_tools>` JSON block at the top of your system
prompt. Use `get_api_spec(server_name="...", tool_name="...")` to fetch
the parameter shape for any specific tool you intend to call.

**Do NOT use provider-native or built-in filesystem / shell tools** —
`Bash`, `Read`, `Write`, `read_file`, `write_file`, `list_directory`,
`grep_search`, `glob`, `read_many_files`, `replace`, `run_shell_command`.
They are disabled when the bridge is active. For filesystem access, use only the tools
declared in this session (typically `execute_shell_command` for reads,
writes, and commands; other workspace tools when explicitly available).

## Environment variables (pre-set)

| Variable | Purpose |
|---|---|
| `$MCP_MCP`, `$MCP_CUSTOM`, `$MCP_VIRTUAL` | Short endpoint bases for MCP, custom, and virtual tools |
| `$MCP_AUTH` | Authorization header value; use `-H "$MCP_AUTH"` |
| `$MCP_API_URL` + `$MCP_API_TOKEN` | Full endpoint + bearer token fallback |
| `$STEP_OUTPUT_DIR` | Write primary outputs here. The folder exists — do not `mkdir` it |
| `$STEP_EXECUTION_DIR` | Parent of `$STEP_OUTPUT_DIR`. Use only as a fallback when reaching a sibling step's folder and `sys.argv` wasn't used |
| `$DB_PATH` | ABSOLUTE path to the workflow's `db/db.sqlite`. Always `sqlite3 "$DB_PATH"` — never the relative `db/db.sqlite` (the shell runs in the step folder, so a relative path resolves to the wrong place or read-only) |
| `$VAR_<NAME>` | Workflow config values (e.g. `$VAR_PAN`, `$VAR_SHEET_URL`). Reference always; never hardcode the literal value |
| `$SECRET_<NAME>` | Credentials (e.g. `$SECRET_API_KEY`). Never echo to stdout, never write to files |
| `$VAR_GROUP_NAME` | Current group (may be empty when no group is active). The only var where empty/absent is acceptable |

**Missing vars must fail loudly.** In bash: `"${VAR_PAN:?missing}"` or
`set -u`. In Python: `os.environ['VAR_PAN']` — never `.get()` with a default.

## Calling an MCP tool

MCP tools live at `$MCP_MCP/{server}/{tool}`. Authenticate with
`-H "$MCP_AUTH"`. Use any shell that fits — curl, jq, node, python,
whatever. Shell-first; Python is optional.

```bash
payload='{"arg1":"value1"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_MCP/{server_name}/{tool_name}" | jq
```

**Response envelope:** `{"success": true|false, "result": ..., "error": "..."}`.
Always check `success` before treating `result` as authoritative.

For retries, backoff, or structured logging, write a small helper in the
language of your choice. For reusable helpers saved to `main.py`
(`scripted` mode only), see the `code-authoring` reference doc.

## Workflow

1. Inspect the `<available_tools>` JSON block. Pick the tool you need.
2. Call `get_api_spec(server_name="...", tool_name="...")` to get the full
   parameter shape if you haven't called this tool before.
3. Use `execute_shell_command` to write and run the code that calls the
   tool via the HTTP bridge.
4. Parse `success`/`result`/`error` from the response. Retry with adjusted
   args on transient failure; bail with a clear message on hard failure.

## Single-call discipline (agentic mode)

The goal of agentic mode is to **minimize tool calls**. Ideally
the entire step runs in a **single `execute_shell_command` call** — one
script that does API calls, data processing, and output writing in one
shot. After a step runs, review whether the agent used multiple tool
calls where a single script would have sufficed.

This is the agentic-mode efficiency target. For multi-call workflows
where each call's output drives the next, that's fine; for independent
operations that could batch, consolidate.

## Variable handling

When writing code that uses workflow variables, **never hardcode the
literal value** — account IDs, URLs, credentials, etc. — even if you can
see the value in the step description. The execution agent's saved
learnings get auto-templated, so hardcoded values become stale placeholders
(`{{"{{"}}VARIABLE_NAME{{"}}"}}`) that fail at runtime when the value changes.

Pattern for code accepting variables:

```python
import os, sys, argparse

parser = argparse.ArgumentParser()
parser.add_argument("--pan", default=os.environ["VAR_PAN"])
parser.add_argument("--sheet-url", default=os.environ["VAR_SHEET_URL"])
args = parser.parse_args()
```

Bash equivalent:

```bash
PAN="${VAR_PAN:?missing}"
SHEET_URL="${VAR_SHEET_URL:?missing}"
```

## Common mistakes

- Calling provider-native `Bash` / `Read` / `Write` tools — disabled; use
  `execute_shell_command` instead.
- Constructing absolute MCP tool URLs without `$MCP_API_URL` — breaks when
  the bridge host changes.
- Echoing `$SECRET_*` values to stdout for debugging — credentials leak
  into logs.
- Using `os.environ.get('VAR', 'default')` — silent fallback masks missing
  config. Use `os.environ['VAR']` and let it raise.
- Splitting a step into 10 tool calls when one script would do — wasted
  tokens; consolidate into a single `execute_shell_command`.
- Hardcoding variable values inline — fails the next time the value
  changes. Always pass via `sys.argv` / `argparse` / env vars.
