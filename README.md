# 🚀 AgentForge

![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/manishiitg/mcp-agent-builder-go)
[![Security Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Secret%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Dependency Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Dependency%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Go Version](https://img.shields.io/badge/Go-1.24.4-blue.svg)](https://golang.org/)
[![React](https://img.shields.io/badge/React-19.1.1-blue.svg)](https://reactjs.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](#license--architecture-foundations)

**AgentForge** is a multi-model agent platform for building, orchestrating, and scheduling AI workflows across coding tools, chat channels, browser automation, and human approvals.

### Install on macOS (Apple Silicon)

```bash
curl -fsSL https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/install.sh | bash
```

Downloads the latest dmg from [Releases](https://github.com/manishiitg/mcp-agent-builder-go/releases/latest), installs to `/Applications`, clears the Gatekeeper quarantine flag, and launches the app. See the [Desktop App section](#-desktop-app-macos) below for details + manual install.


Run **Claude Code, Codex, Gemini CLI, and open models** in one system. Build visual workflows, launch complex orchestrators, schedule recurring jobs, and route agent conversations through **Slack, WhatsApp, and the web**.

AgentForge is built for teams that want more than a chat box:
- Build visual agent workflows and long-running orchestrators
- Mix and match the best coding and reasoning models for each step
- Schedule automations, recurring jobs, and background runs
- Keep humans in the loop with approvals, feedback, and escalation paths
- Connect agents to Slack, WhatsApp, browsers, Google Workspace, and MCP tools

## Why AgentForge

- **Multi-model by default**: Use Claude Code, Codex, Gemini CLI, OpenAI, Anthropic, Bedrock, Azure, MiniMax, OpenRouter, and open models in the same platform.
- **Visual workflows plus real execution**: Design workflows on a canvas, then run them with tools, browser automation, memory, and evaluation built in.
- **Built for operations, not demos**: Add scheduling, observability, validation, approvals, and secure workspace isolation from day one.
- **Protocol-agnostic in practice**: MCP is supported, but AgentForge is broader than any single protocol, provider, or model vendor.

## What You Can Build

- **Coding workflows** that delegate across Claude Code, Codex, Gemini CLI, and open-source coding models
- **Scheduled automations** for research, support, reporting, or back-office operations
- **Human-in-the-loop agents** that pause for approvals, 2FA codes, or operator feedback
- **Slack and WhatsApp agents** that continue conversations outside the dashboard
- **Browser-powered workflows** that log in, click through apps, collect data, and complete tasks

## Works With

### Coding and LLM Models

- **Claude Code** via the `@anthropic-ai/claude-code` CLI
- **Codex-style agentic models** through OpenAI and Azure AI Foundry
- **Gemini CLI** via the `@google-gemini/gemini-cli`
- **Open-source and frontier models** through OpenRouter, Bedrock, Vertex AI, and direct provider integrations

### Channels, Tools, and Connectors

- **Slack**, **WhatsApp**, and custom webhook-based chat surfaces
- **Google Workspace** for Gmail, Drive, Calendar, Docs, and Sheets
- **Browser automation** through Vercel Agent-Browser, Playwright, and local CDP bridging
- **MCP servers**, local tools, workspace files, and custom connectors

## Why Teams Choose It

- Replace brittle prompt chains with durable workflows
- Use the right model for the right step instead of standardizing on one vendor
- Bring coding agents, operational automations, and human approvals into one system
- Ship agent workflows that can be monitored, evaluated, and improved over time

---

## ⚡ Platform Overview

At the core of AgentForge is the **[workflow system](docs/workflow/README.md)**, a directed step-based workflow runtime managed through the visual workflow builder.

Design complex workflows visually, refine them through the interactive builder, then run them with step-level configuration, tiered LLM selection, deterministic pre-validation, evaluation runs, scheduling, cost tracking, and persistent run data.

### 🧠 Learning, Validation, and Observability
Move beyond static prompts with built-in optimization, validation, and run visibility.

- **[Learning Architecture](docs/workflow/learning_architecture.md):** Workflow learning now centers on a shared global skill plus step-level metadata and saved scripts for scripted steps.
- **[Deterministic Pre-Validation](docs/workflow/pre_validation_guide.md):** A high-speed, code-based validation layer that uses JSON schemas and consistency rules to verify artifacts with zero token cost and absolute precision.
- **[Evaluation & Benchmarking](docs/workflow/evaluation_system.md):** A dedicated testing suite that executes workflows in isolated environments to generate performance, cost, and accuracy metrics—essential for production readiness.
- **[Continuous Observability](docs/workflow/workflow_monitoring.md):** Execution logs, costs, evaluation reports, learnings, and run history across workflow, run-folder, and scheduled-run views.
- **[Cost and Log Measurement](docs/workflow/cost_and_log_measurement.md):** Token usage, model cost, and execution logs are tracked across workflow phases, runs, steps, and models.
- **[Persistent Stores](docs/workflow/persistent_stores_design.md):** Workflows can persist structured run data for reports, knowledgebase updates, and follow-up analysis.
- **[Swarm Delegation](docs/multiagent/sub_agent_delegation.md):** Empower your primary agent to dynamically spawn independent sub-agents, parallelizing complex research, coding, or data extraction tasks across a distributed swarm.
- **[Task Orchestration](docs/workflow/todo-task-step-type.md):** Intelligent sub-task routing that manages state, dependencies, and context windows automatically.

### 🛡️ Security and Guardrails
Deploy with deterministic controls designed for strict environments.
- **[FolderGuard](docs/core/folder_guard_system.md):** Runtime read/write validation wraps workspace tools so agents only touch the folders each mode or step is allowed to access.
- **[Multi-User Authentication & Workspace Isolation](docs/core/multi_user_authentication.md):** Per-user workspace isolation, user-scoped paths, and sandboxed shell execution protect users from cross-tenant contamination.
- **[Secrets](docs/core/secrets.md):** Securely inject credentials into agent queries, workflow steps, and delegated agents without exposing them in chat history or logs.
- **[Restricted Configuration Mode](docs/core/env-api-key-defaults.md):** Optionally lock provider/model configuration so the server uses environment-injected API keys (`LLM_CONFIG_LOCKED`) and secrets never reach the browser.
- **[Secure MCP OAuth](docs/core/oauth.md):** Seamless, auto-discovering OAuth 2.0 flows for connecting enterprise MCP servers safely.

### 👁️ Automation, Connectors, and Browser Control
Connect agents to real systems and communication channels.
- **[Google Workspace (GWS)](docs/core/google_workspace_integration.md):** Native CLI injection grants agents deterministic, scoped access to Gmail, Drive, Calendar, Docs, and Sheets.
- **[Vercel Agent-Browser](https://github.com/vercel-labs/agent-browser):** High-level browser automation engine used for complex web interactions, DOM analysis, and visual grounding.
- **[Browser System](docs/core/browser.md):** Covers browser session management, runtime limits, and browser integration patterns across providers.
- **[Bot Connectors](docs/core/bot_connector_system.md):** Expose specialized agent sessions through Slack, WhatsApp, the web simulator, and custom connector surfaces.
- **[Workflow Scheduling](docs/workflow/workflow_scheduling.md):** Run workflows on recurring schedules with history, routing, and run-state tracking.
- **[Native Workspace Mode](docs/core/native_workspace_mode.md):** Run workspace operations directly against local folders when native execution is preferred over containerized workspace mode.

### 🤝 Human-in-the-Loop Operations
Keep operators involved when workflows need approval, intervention, or additional input.

- **[Human Feedback System](docs/workflow/human_feedback_system.md):** Agents can pause execution to request explicit approval, 2FA codes, or strategic guidance via real-time browser notifications or the visual dashboard.
- **[Slack Human Connector](docs/workflow/human_feedback_system.md#slack-configuration):** 
    - **Smart Delayed Notifications**: If a user doesn't respond in the UI within 2 minutes, the orchestrator automatically pings a configured Slack channel.
    - **Threaded Conversations**: Users can reply directly in the Slack thread to provide the required information, which is then fed back to the agent's context in real-time.
    - **Multi-User Collaboration**: Entire teams can monitor agent progress and intervene via Slack without ever opening the dashboard.

---

### 🧩 LLM Configuration and Providers

AgentForge is provider-agnostic. Users configure published LLMs in the UI, then assign them to chat sessions, workflow phases, and workflow tiers.

- **[LLM Configuration & Resilience](docs/core/llm_configuration_and_resilience.md):** Published LLMs carry provider, model, API key, temperature, and model-specific options; the backend does not require provider keys at startup in the default mode.
- **[Tiered LLM Allocation](docs/workflow/tiered_llm_allocation.md):** Workflow steps can use tiered model selection, with separate phase LLM configuration for planning, builder, evaluation, and debugging-style phase work.
- **[Azure AI Foundry](docs/core/azure_foundry_integration.md):** Azure OpenAI and Responses API routing are supported for newer agentic model deployments.
- **[Environment-Based Defaults](docs/core/env-api-key-defaults.md):** Optional defaults and locked server-side configuration are available for managed deployments.
- Providers include OpenAI-compatible endpoints, Anthropic, Google Gemini/Vertex, AWS Bedrock, Azure AI Foundry, MiniMax, OpenRouter, and local/CLI-backed agent integrations.

#### 🛠️ Local CLI Agents
Bring your existing CLI-based coding agents into the visual orchestrator via the **[MCP Bridge Layer](docs/core/mcp_bridge_layer.md)**:
*   **Claude Code**: Native integration with the `@anthropic-ai/claude-code` CLI.
*   **Gemini CLI**: Integration with the `@google-gemini/gemini-cli`.
*   **State Persistence**: Support for `--resume` functionality, allowing the visual orchestrator to maintain long-running coding sessions across CLI restarts.

---

## 💻 Desktop App (macOS)

A standalone macOS app is available — no Docker, no manual server setup. Each release is published at [Releases](https://github.com/manishiitg/mcp-agent-builder-go/releases/latest).

### Install (one-liner — recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/install.sh | bash
```

Downloads the latest dmg, installs `Runloop.app` to `/Applications`, strips the macOS quarantine flag (no "damaged" warning), and launches the app. Pin a specific version with `RUNLOOP_VERSION=v1.25.6 curl -fsSL … | bash`.

### Install manually

1. Download `Runloop-<version>-arm64.dmg` from the latest release.
2. Open the dmg, drag **Runloop** to Applications.

### First-launch error: *"Runloop is damaged and can't be opened"*

The current build is **unsigned and not notarized**, so macOS Gatekeeper flags it on download. The app itself is fine — you just need to clear the quarantine flag macOS automatically attaches to downloaded files.

**Recommended — Terminal (works on all macOS versions):**
```bash
xattr -cr /Applications/Runloop.app
```
Then double-click Runloop. If macOS still complains, also strip the dmg you downloaded:
```bash
xattr -cr ~/Downloads/Runloop-*.dmg
```
`sudo` is **not** needed — you own the app since you dragged it into Applications.

**System Settings (sometimes works, depends on macOS version):**
For "damaged" verdicts on Sequoia/Tahoe, macOS often hides the "Open Anyway" button entirely, so this path frequently doesn't appear. If it does:
1. Open **System Settings → Privacy & Security**.
2. Scroll to the Security section. If you see *"Runloop was blocked from use…"* with an **Open Anyway** button, click it.
3. Confirm in the dialog. macOS remembers the decision.

If the button isn't there, fall back to the `xattr` command above.

### First-launch UX

On first run the app prompts for two things:
1. **Workspace folder** — pick where your `workspace-docs/` lives (skills, configs, schedules, WhatsApp DB, encrypted provider keys). Defaults to `~/Library/Application Support/runloop-desktop/workspace-docs/`.
2. **AUTH_SECRET** — the secret used to encrypt `provider-api-keys.json`. If you're moving from a previous setup, enter the same secret you used there. Otherwise pick a strong value and remember it (you'll need it on every machine that opens this workspace).

After that, add provider API keys (OpenAI, Gemini, Anthropic, etc.) through the in-app provider auth flow. They are encrypted at rest in `<workspace-docs>/config/provider-api-keys.json`.

### Why no signing?

Code signing + Apple notarization requires an Apple Developer ID ($99/yr) and is on the roadmap. Until then, the manual quarantine step is unavoidable on first install.

---

## 🚀 Quick Start (Local Development)

### 1. Prerequisites

- Go 1.24+
- Node.js 20+ and npm
- Optional local tools depending on what you enable: Claude Code, Gemini CLI, Codex-compatible CLIs, browser tooling, AWS/GCP CLIs, etc.

### 2. Clone and Configure

```bash
git clone https://github.com/manishiitg/mcp-agent-builder-go.git
cd mcp-agent-builder-go
cp agent_go/env.example agent_go/.env
```

Edit `agent_go/.env` for local app/runtime settings if needed. LLM providers and API keys are configured from the app UI after startup, not by editing the README examples into `.env`.

Install dependencies:

```bash
cd frontend
npm ci

cd ../agent_go
go mod download
```

### 3. Run Everything Locally

Start the backend, workspace API, frontend, and Electron with one command from `agent_go/`:

```bash
cd agent_go
./run_server_with_logging.sh --with-workspace --with-frontend
```

Default local ports:

| Service | Default URL |
| --- | --- |
| Agent API | `http://localhost:18743` |
| Workspace API | `http://localhost:18744` |
| Frontend | `http://127.0.0.1:51733` |

The runner prefers these ports. If a port is already occupied, it picks the next available port and prints the final URL. Logs are written to `agent_go/logs/`.

### 4. Frontend-Only Development

Use this when the backend and workspace API are already running:

```bash
cd agent_go
./run_server_with_logging.sh --only-frontend
```

This starts Vite plus Electron. It reads `AGENT_PORT` and `WORKSPACE_PORT` from `frontend/public/runtime-config.js` when that file already exists.

### 5. Frontend Build Mode

Use this to run the frontend like a production static build, without Vite hot reload:

```bash
cd agent_go
./run_server_with_logging.sh --only-frontend --build
```

This builds `frontend/`, serves the static output on the frontend port, and launches Electron against that static server.

You can override ports explicitly:

```bash
AGENT_PORT=18743 WORKSPACE_PORT=18744 FRONTEND_PORT=51733 ./run_server_with_logging.sh --only-frontend --build
```

### 6. Stop and Restart Cleanly

When the runner is in the foreground, press `Ctrl+C`. The script stops child processes and prints which ports were released.

If startup says a port is still busy, inspect it:

```bash
lsof -nP -iTCP:51733 -sTCP:LISTEN
lsof -nP -iTCP:18743 -sTCP:LISTEN
lsof -nP -iTCP:18744 -sTCP:LISTEN
```

### 7. Debug Local API Traffic

Backend request logs are written under `agent_go/logs/`. The server logs API start/end lines, including status code and duration, which is useful when the frontend appears stuck or too many requests are firing at once.

Useful checks:

```bash
curl -fsS http://localhost:18743/api/health
curl -fsS http://localhost:18744/api/health
```

### 8. Validation Commands

```bash
# Backend compile check
cd agent_go
go test ./cmd/server -run '^$'

# Frontend type check
cd frontend
./node_modules/.bin/tsc -b
```

---

## ☁️ Production Deployment Topologies

Deploy your agentic infrastructure where it makes sense for your security posture.

### **1. Azure Virtual Machine (Maximum Security Isolation)**
The recommended topology for enterprise deployments. Leverages Azure VMs to utilize deep Linux kernel features (namespaces, `unshare`) for absolute filesystem isolation between agent runs.
```bash
cd deploy/azure/terraform
terraform init && terraform apply
cd .. && ./deploy_vm.sh <VM_IP_ADDRESS> all
```
> **[Read the Azure VM Deployment Blueprint](deploy/azure/README.md)**

### **2. Kubernetes (High-Availability Swarms)**
Designed for massive scale and resilience using standard Helm-like manifests.
```bash
./deploy/k8s/scripts/deploy-k8s.sh --build
```
> **[Read the Kubernetes Deployment Blueprint](deploy/k8s/README.md)**

---

## 🤝 Join the Revolution

We are building the future of deterministic AI orchestration. Contributions are highly encouraged!

```bash
# Setup development guardrails
./scripts/install-git-hooks.sh

# Run the Go orchestration test suite
cd agent_go && go test ./...

# Audit for secrets
./scripts/scan-secrets.sh
```

## 📄 License & Architecture Foundations

Licensed under the MIT License.

**Built Upon:**
- **[Model Context Protocol (MCP)](https://modelcontextprotocol.io/):** The universal standard for AI tool integration.
- **[LangChain Go](https://github.com/tmc/langchaingo):** High-performance LLM routing.
- **[React Flow](https://reactflow.dev/):** The industry standard for node-based visual editing.
