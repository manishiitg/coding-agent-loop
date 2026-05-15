# Agent Builder Testing

This folder is for repo-level Agent Builder test harnesses. It is intentionally
separate from `agent_go/cmd/testing`, which tests the lower-level agent runtime.

## Workflow Builder Message Queue

`workflow_builder_sequence.py` sends one or more user messages to a Workflow
Builder session using the same `/api/query` endpoint as the frontend.

Start the app first:

```bash
cd agent_go
./run_server_with_logging.sh --with-workspace --with-frontend
```

Run a message queue:

```bash
python3 cmd/testing/workflow_builder_sequence.py \
  --workspace-path "Workflows/My Workflow" \
  --messages-file cmd/testing/examples/message_sequence_smoke.json
```

Useful flags:

- `--base-url`: Agent API URL. Defaults to `http://localhost:18743`.
- `--session-id`: Reuse a Builder session. Defaults to a generated session id.
- `--preset-query-id`: Optional preset id. Defaults to the workspace path so
  manifest-backed workflows can still run without a database preset lookup.
- `--group-name`: Repeatable. Passed as enabled workflow variable groups.
- `--workshop-mode`: `builder`, `optimizer`, `run`, or `reporting`.
- `--dry-run`: Print requests without sending them.

The harness waits for each Builder turn to complete before sending the next
message. This is useful for testing message-sequence behavior, including
resume-style turns that call `execute_step(..., human_input=...)`.
