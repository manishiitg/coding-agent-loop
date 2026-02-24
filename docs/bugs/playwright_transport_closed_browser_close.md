# Bug: Playwright MCP — "failed to call tool browser_close: transport error: transport closed"

## Status: Known / Upstream

## What you see

When calling the Playwright MCP tool `browser_close` (or sometimes other browser tools), the call fails with:

```text
failed to call tool browser_close: transport error: transport closed
```

## What it means

**"Transport closed"** means the **connection** between the agent and the Playwright MCP server (the stdio pipe to the `npx @playwright/mcp` process) is already closed. So the tool call never reaches the server, or the server’s reply can’t get back.

So the error is **not** that `browser_close` is invalid — it’s that the **transport (the MCP connection) is gone** by the time the call runs.

## Typical causes

1. **Playwright MCP process exited**
   - The Node process running `@playwright/mcp` crashed or exited (e.g. uncaught exception, OOM, or it shuts down after closing the browser in some flows).
   - Once the process exits, the stdio pipes close → next tool call gets "transport closed".

2. **Connection was closed on our side**
   - Something called `CloseSession(sessionID)` or closed the MCP client (e.g. workflow/step ended and session was closed).
   - The agent or a later step then tries to call `browser_close` on the same session → connection already closed → "transport closed".

3. **Browser / process died earlier**
   - The browser or Playwright process died (e.g. killed, crash).
   - The MCP server might then close the transport or exit.
   - A subsequent `browser_close` (or any tool) then sees "transport closed".

4. **Timeout or kill**
   - The subprocess was killed (e.g. timeout, OOM killer, user interrupt).
   - Pipes close → "transport closed" on the next use.

So in practice: **something already closed or killed the Playwright MCP connection; `browser_close` is just the call that happened to run after that.**

## What to do

- **If the browser is already closed / you’re just cleaning up**  
  The connection may have been closed when the browser closed or when the server exited. You can treat "transport closed" in that case as “connection already gone” and continue (e.g. next run will create a new connection). No change needed on your side.

- **If you need a fresh browser session**  
  Start a new workflow run or a new chat session so a new Playwright MCP connection (and browser) is created. With session reuse, the same connection is used until `CloseSession` or process exit.

- **If it happens often mid-workflow**  
  Check server logs for Playwright/subprocess crashes or early exit. Ensure the process isn’t being killed (timeouts, OOM). If you’re calling `CloseSession` (or closing the session) before all browser steps finish, avoid closing the session until after the last browser tool call.

- **Retries**  
  The client can treat "transport closed" as a non-retryable connection failure: the process is gone, so retrying the same connection doesn’t help. Next request that needs Playwright will create a new connection (new session or after session close).

## Do we create a new connection or keep returning the same error?

**Before (without fix):** We did **not** treat "transport error: transport closed" as a reconnection trigger. The agent (or HTTP executor) kept using the same dead client reference, so **subsequent tool calls kept failing** with the same error until you started a new run/session (new agent → new connections).

**After (with fix):** We now treat **"transport closed"** as a broken-pipe–style error:

- **`mcpclient.IsBrokenPipeError`** returns true for errors containing `"transport closed"` (in addition to broken pipe, EOF, etc.).
- **Agent path:** When a tool call fails with that error, the broken-pipe handler runs: it closes the old client, gets a **fresh connection** (new Playwright process), updates the agent's client map, and **retries the tool call once**. So we **do** create a new connection and retry; later tool calls use the new connection.
- **HTTP executor path:** Same: on "transport closed" we get a fresh connection and retry once.

So after the fix, the **first** failure with "transport closed" triggers a reconnect and one retry; you don't keep seeing the same error for the same agent/session unless the retry also fails or the new process dies again.

**Registry:** When a **new** agent is created and calls `GetOrCreateConnection`, the registry **Pings** the existing connection; if it's dead, it removes it and creates a new one. So new agents get a live connection even if the registry still had a dead reference.

## Relation to other Playwright bugs

- **Downloads/screenshots in wrong folder** → see `docs/bugs/playwright_download_files_wrong_location.md` (fixed via `WorkingDir` and runtime override).
- **"Browser is already in use"** → use `--isolated` in Playwright MCP config; see `docs/running_workflows.md` / historical records.
- **Session not found (HTTP transport)** → applies to streamable-http; we use stdio, so "transport closed" is the usual stdio analogue when the connection is gone.

## References

- mcp-go client returns "transport error: transport closed" when the stdio transport’s pipes are closed (process exited or connection closed).
- Upstream Playwright MCP: [browser state not released after browser_close (#1245)](https://github.com/microsoft/playwright-mcp/issues/1245), [Session not found / transport (#1307, #1140)](https://github.com/microsoft/playwright-mcp/issues/1307).
