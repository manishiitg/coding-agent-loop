# Tool Search Mode

## Overview

Tool search mode is a runtime capability for agents that should discover tools on demand instead of loading a large tool catalog up front.

This is better treated as a **core/platform feature**, not a workflow feature:

- it affects agent tool exposure and prompt behavior
- it can be used outside workflow execution
- workflow step config no longer treats it as a normal per-step setting

## Current State

What is still true in the current codebase:

- tool search mode still exists as a runtime concept and event field
- workshop and workflow prompt paths still distinguish between code execution mode and tool search mode
- tool-search-style behavior is relevant for non-code-exec agents
- learning agents explicitly do **not** use tool search mode

What changed:

- workflow step config no longer has first-class `use_tool_search_mode` or `pre_discovered_tools` fields in `AgentConfigs`
- the workflow step editor cleans up those legacy fields if they are present in saved JSON
- the old workflow doc describing step-level configuration is stale

## Why It Moved Out Of Workflow

Tool search mode is now closer to platform/runtime plumbing than workflow design:

- it is not part of the current workflow step configuration model
- it is mainly about how an agent receives and discovers tools
- workflow docs should focus on active workflow-only behavior such as `todo_task`, learn-code, learning, and pre-validation

## Workflow Reality

For workflows today:

- code-exec and `learn_code` are the main execution-mode docs that matter
- step-level tool search fields are legacy and should not be documented as active workflow configuration
- prompt code still contains tool-search branches because not every workflow agent is code-exec

One concrete indicator of this shift:

- [StepEditPanel.tsx](/Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/events/orchestrator/StepEditPanel.tsx#L619) deletes legacy `use_tool_search_mode` and `pre_discovered_tools` before saving step config

## Where It Still Shows Up In Code

- [server.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/server.go#L2369) still loads tool-search-related preset/runtime context for workflow agents
- [server.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/server.go#L4571) still uses the code-exec vs tool-search distinction for workshop prompt setup
- [controller_execution.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go#L1183) still has prompt behavior that differs in tool search mode
- [controller_agent_factory.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go#L1294) explicitly says learning agents do not use code execution mode or tool search mode
- [prompt_sections.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/prompt_sections.go#L16) still treats code-exec/tool-search instructions as a shared prompt concern
- [data.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/events/data.go#L68) still carries `use_tool_search_mode` in event data

## Documentation Guidance

Use this doc as the canonical reference if you need to explain:

- what tool search mode is
- why it still appears in runtime code and events
- why it should not be presented as an active workflow step feature

Do not use this doc to justify adding `use_tool_search_mode` back into normal workflow step docs unless the workflow editor and step config model are restored to support it again.
