## MESSAGE SEQUENCE ROUTE PATTERNS

Use these patterns when designing or hardening todo_task predefined routes:

- **Stateful Specialist**: one todo_task route owns an expert conversation the orchestrator can re-enter across feedback loops.
- **Test/Fix Loop**: orchestrator calls the same message_sequence route, runs validation/tests, then re-enters it with failures instead of starting over.
- **Maker + Reviewer**: one route creates output and a separate message_sequence reviewer route remembers standards, prior issues, and review history.
- **Panel of Specialists**: several message_sequence routes keep separate memory for different domains while the todo_task orchestrator coordinates decisions.
- **Clean-Room Retry**: use `message_sequence_restart=true` when a clean second attempt is needed because prior context is stale, wrong, or contaminated.
- **Human-in-the-Loop Re-entry**: human_input or operator feedback can be sent back into the same sequence conversation as the next user message.
- **Top-Level Scripted Conversation**: use a top-level message_sequence when the workflow is a fixed linear conversation and does not need orchestration.

For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory across critique, test feedback, validation feedback, or follow-up calls. Reuse the same route for re-entry; restart only when the prior conversation is stale, wrong, or contaminated.

Route sub-agents can be `regular` for stateless one-off work, `message_sequence` for a stateful specialist conversation, or `generic_agent` for an agent with broad workspace access. Normal repeated calls reuse the route session — the orchestrator sends the next re-entry user message to the same conversation. Set `message_sequence_restart=true` only when starting fresh is required (stale, contaminated, or wrong-direction conversations). As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent.
