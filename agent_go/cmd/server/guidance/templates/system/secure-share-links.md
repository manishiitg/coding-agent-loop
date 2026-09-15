# Secure file and folder share links

Use this reference when a user wants a link to an existing workflow artifact
without publishing it. These are authenticated AgentWorks preview links, not
public hosting.

## Create a report link

For the active workflow's dashboard, call `get_report_link` with no arguments:

```json
{}
```

The server verifies access and confirms that `db/reports/index.html` exists,
then returns a dedicated `/report` URL. Present its `url` value verbatim. This
viewer uses the same report runtime as Builder, including the report's styling,
tabs, live data API, file actions, and refresh behavior. Do not use
`get_file_link` for the dashboard: its restricted generic HTML preview is not
the report runtime.

## Create a file or folder link

Call `get_file_link` with a canonical path relative to the active workflow:

```json
{"path":"db/reports/index.html"}
```

The server checks that the caller can still access the workflow, rejects
traversal and protected paths, verifies that the target exists, detects whether
it is a file or folder, and returns the correct `/file` or `/folder` preview URL.
Do not manually Base64-encode paths or construct these URLs in a prompt or
script.

Present the returned `url` value verbatim as the clickable link. Do not rewrite
it into `/file/<base64>` or `/folder/<base64>` and do not manually encode a
path. `preview_url` is retained as a compatibility alias for `url`.

The returned URL contains no password, token, or access grant. A
recipient must sign in to AgentWorks and already have access to the workflow.
Removing that access also removes their ability to open the link. Never claim
that creating the link shared the workflow with a recipient.

Use the workflow access tool separately only when the user explicitly asks to
change who can access the workflow. Use the publish flow instead when the user
wants anonymous, externally hosted, or password-gated public distribution.

## Scripted steps

Load `references/mcp-bridge.md` before authoring bridge code. `get_file_link` is
a custom tool, so call it through `$MCP_CUSTOM/get_file_link`:

```bash
artifact_path='runs/latest/report.pdf'
payload="$(jq -cn --arg path "$artifact_path" '{path:$path}')"
curl --fail-with-body -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/get_file_link"
```

Check the bridge response's `success` field before using its `result`. Parse the
tool result as JSON and use `url` (or legacy `preview_url`). Persist or send that URL only when the
workflow contract requires it. A scripted step must pass the artifact's current
workflow-relative path; it cannot create links for another workflow or for an
arbitrary web URL.

For the workflow dashboard, use the same bridge pattern with an empty payload
and the report tool:

```bash
payload='{}'
curl --fail-with-body -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/get_report_link"
```
