# Documentation

Start with the operator journey, then use the subsystem references when you need implementation detail.

## Start Here

- [Getting Started](getting-started/README.md): install AgentWorks, complete first-launch setup, and create a first automation.
- [Build Your First Workflow](getting-started/first-workflow.md): define an outcome, choose a worker, run it, review evidence, and improve the next run.

## Product Areas

- [Workflow](workflow/README.md): workflow authoring, execution, scheduling, monitoring, Pulse, and Auto Improve.
- [Organization and Agents](multiagent/README.md): delegation, Org Pulse, shared memory, and agent-to-agent coordination.
- [Core](core/README.md): providers, MCP, browser sessions, connectors, secrets, security, and shared runtime services.

`docs/bugs/` is an incident archive. `docs/refactor/` records implementation migrations. Neither folder is the recommended entry point for operators.

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

### Organization and Agents

- Sub-agent delegation
- Multi-agent chat architecture
- Agent memory
- Slash-command entry points for delegation

### Core

- Eventing, streaming, session propagation, auth, secrets, and workspace isolation
- LLM provider configuration and resilience
- Browser, MCP bridge, bot connectors, and external integrations
- Platform plans and operational system docs

## Validate Links

Run the documentation link check before merging documentation changes:

```bash
node scripts/check-doc-links.js
```
