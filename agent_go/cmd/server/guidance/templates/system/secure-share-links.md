# Secure file and folder share links

Use this reference when a user wants a link to an existing workflow artifact
without publishing it. These are authenticated AgentWorks preview links, not
public hosting.

## Create a link

Call `get_file_link` with a canonical path relative to the active workflow:

```json
{"path":"db/reports/index.html"}
```

The server checks that the caller can still access the workflow, rejects
traversal and protected paths, verifies that the target exists, detects whether
it is a file or folder, and returns the correct `/file` or `/folder` preview URL.
Do not manually Base64-encode paths or construct these URLs in a prompt or
script.

The returned `preview_url` contains no password, token, or access grant. A
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
tool result as JSON and use `preview_url`. Persist or send that URL only when the
workflow contract requires it. A scripted step must pass the artifact's current
workflow-relative path; it cannot create links for another workflow or for an
arbitrary web URL.
