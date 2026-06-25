# Runloop Public Roadmap

This roadmap describes the main product directions for Runloop. It is not a release contract; priorities can change as users report issues, provider APIs evolve, and the workflow runtime matures.

## Current Focus

- Make the macOS desktop install path reliable through the recommended one-line curl installer.
- Improve first-run setup so users can start with an installed coding-agent CLI before manually configuring every provider.
- Make the public repo easier to understand through examples, clearer docs, and issue-backed roadmap work.

## Near-Term Priorities

### 1. First-Run Onboarding

Runloop should feel usable immediately after install.

Planned work:
- Detect installed coding-agent CLIs such as Claude Code, Codex, Gemini CLI, Pi CLI, and Antigravity CLI.
- Create sensible default LLM configuration when one usable CLI is found.
- Show setup diagnostics for required runtime pieces such as `mcpbridge`, Go, `tmux`, provider auth, and workspace configuration.
- Let users start a chat or build a workflow without manually wiring every provider first.

### 2. Memory-Aware Multi-Agent Chat

Multi-agent chat should become a durable command and reporting plane for workflows.

Planned work:
- Remember user preferences such as preferred models, connector choices, workspace habits, and recurring workflow decisions.
- Ground chat answers in workflow runs, schedules, reports, knowledgebase outputs, and prior builder decisions.
- Let users ask operational questions such as "what failed last night?", "which workflows need attention?", and "what changed since the last run?"
- Keep memory inspectable and editable so users can correct bad assumptions.

### 3. Workflow Notifications and Escalation

Workflows should be able to notify and involve humans through the same connector system used by bot chats.

Planned work:
- Add workflow-level notification policies for start, completion, failure, timeout, approval needed, monitor finding, and recovery events.
- Route notifications to in-app UI, Slack, WhatsApp, and future connectors.
- Wake the main builder agent with full context when a workflow failure needs diagnosis.
- Send concise human-facing summaries while preserving detailed logs for agents.
- Make escalation configurable per workflow, step, schedule, and connector destination.

### 4. Anthropic Agent SDK Runtime

Runloop should support Anthropic's Agent SDK as a first-class runtime alongside CLI and API-backed agents.

Planned work:
- Map Runloop tools, MCP access, memory, permissions, streaming, and resume semantics onto Claude-native agent execution.
- Support workflow and chat sessions that can choose the Agent SDK runtime where it is the best fit.
- Preserve Runloop observability, event history, and human-in-the-loop behavior across the new runtime.

### 5. Pi CLI and Cost-Effective Model Support

Pi CLI should become the preferred multi-model coding-agent runtime with dependable tmux behavior and model coverage.

Planned work:
- Improve Pi terminal, resume, and workspace behavior to match the expectations set by Claude Code, Codex, and Gemini CLI integrations.
- Add and certify support for Google/Pi-routed models and other cost-effective coding models.
- Provide low-cost default plans for users who want useful workflows without frontier-model pricing.
- Document which providers support tool access, resume, streaming, and long-running workflow execution.

### 6. Goal Tracking for Chats and Workflows

Long-running agents need explicit objectives that survive across turns and background work.

Planned work:
- Add `/goal`-style objective tracking for chats and workflows.
- Store success criteria, progress state, blockers, completion checks, and final outcomes.
- Expose goal state in the UI so users can see whether the agent is still aligned.
- Let workflows use goals as a durable contract for scheduled runs and post-run evaluation.

### 7. Tool-Call and Runtime UI/UX

Tool execution should be easy to scan without exposing noisy implementation details.

Planned work:
- Replace raw payload-heavy views with clear action summaries, grouped phases, and useful status.
- Redact auth tokens and other sensitive implementation details by default.
- Keep advanced debug details available when users need to diagnose failures.
- Make terminal, prior chat context, and live workflow state feel connected instead of separate surfaces.

## Later / Exploratory

- Apple Developer ID signing and notarization for the macOS desktop app.
- More connector destinations such as email, Discord, Telegram, and custom webhooks.
- Public workflow templates that users can import directly.
- More GitHub issue automation for roadmap tasks, examples, and beginner-friendly contributions.
- Expanded deployment options for teams that want managed, locked-down, or multi-user installations.

## Issue Tracking

The roadmap should be mirrored into GitHub issues over time. A good issue should include:

- The user-facing problem.
- The current limitation.
- The intended behavior.
- The first concrete implementation step.
- A small verification plan.
