# Documentation

The docs are organized into three product-facing areas:

- [Workflow](workflow/README.md): visual workflows, step types, execution, validation, monitoring, and workflow runtime behavior.
- [Multiagent](multiagent/README.md): delegation, multi-agent chat, shared memory, and multi-agent-specific coordination patterns.
- [Core](core/README.md): platform infrastructure, integrations, security, events, providers, and runtime building blocks used across modes.

`docs/bugs/` stays separate as an incident archive. It is useful history, but it should not define the main product taxonomy.

## Placement Rules

- Put a doc in `workflow/` when it is primarily about workflow authoring, workflow execution, step configuration, or workflow-only UX.
- Put a doc in `multiagent/` when it is primarily about manager/worker delegation, multi-agent chat, or agent-to-agent coordination.
- Put a doc in `core/` when it applies across chat, workflow, and multi-agent modes or describes a foundational subsystem or integration.

## Current Sections

### Workflow

- React Flow canvas and workflow UX
- Running workflows and workflow manifests
- Step config, execution modes, tool filtering, and runtime overrides
- Validation, learning, evaluation, human feedback, and monitoring
- Specialized workflow step types such as `todo_task`

### Multiagent

- Sub-agent delegation
- Multi-agent chat architecture
- Agent memory
- Slash-command entry points for delegation

### Core

- Eventing, streaming, session propagation, auth, secrets, and workspace isolation
- LLM provider configuration and resilience
- Browser, MCP bridge, bot connectors, and external integrations
- Platform plans and operational system docs
