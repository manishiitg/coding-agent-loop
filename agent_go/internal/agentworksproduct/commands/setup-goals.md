Call get_workflow_command_guidance(kind="setup-goals", focus="{{context}}") and follow the returned instructions verbatim.
Treat focus as the conversation/request context that appeared before the slash command, including the user's recent constraints and intent.
The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.
