# 🚀 AgentForge

![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/manishiitg/mcp-agent-builder-go)
[![Security Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Secret%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Dependency Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Dependency%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Go Version](https://img.shields.io/badge/Go-1.24.4-blue.svg)](https://golang.org/)
[![React](https://img.shields.io/badge/React-19.1.1-blue.svg)](https://reactjs.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**AgentForge** is a multi-model agent platform for building, orchestrating, and scheduling AI workflows across coding tools, chat channels, browser automation, and human approvals.

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

At the core of AgentForge is the **[workflow system](docs/workflow/README.md)**, a directed acyclic graph (DAG) engine managed through a **[React Flow Canvas](docs/workflow/react_flow_workflow_canvas.md)**.

Design complex workflows visually, then run them with a 7-phase execution pipeline and 13 specialized node types for planning, routing, evaluation, loops, human input, and execution settings.

### 🧠 Learning, Validation, and Observability
Move beyond static prompts with built-in optimization, validation, and run visibility.

- **[Learning Architecture](docs/workflow/learning_architecture.md):** Workflow learning now centers on a shared global skill plus step-level metadata and saved scripts for scripted steps.
- **[Deterministic Pre-Validation](docs/workflow/pre_validation_guide.md):** A high-speed, code-based validation layer that uses JSON schemas and consistency rules to verify artifacts with zero token cost and absolute precision.
- **[Evaluation & Benchmarking](docs/workflow/evaluation_system.md):** A dedicated testing suite that executes workflows in isolated environments to generate performance, cost, and accuracy metrics—essential for production readiness.
- **[Continuous Observability](docs/workflow/workflow_monitoring.md):** Execution logs, costs, evaluation reports, learnings, and run history across workflow, run-folder, and scheduled-run views.
- **[Swarm Delegation](docs/multiagent/sub_agent_delegation.md):** Empower your primary agent to dynamically spawn independent sub-agents, parallelizing complex research, coding, or data extraction tasks across a distributed swarm.
- **[Task Orchestration](docs/workflow/todo-task-step-type.md):** Intelligent sub-task routing that manages state, dependencies, and context windows automatically.

### 🛡️ Security and Guardrails
Deploy with deterministic controls designed for strict environments.
- **[Zero-Trust Workspace (FolderGuard)](docs/core/multi_user_authentication.md):** Strict per-user filesystem isolation utilizing Linux namespaces. Agents operate in sandboxed environments, preventing cross-tenant data contamination.
- **[Restricted Configuration Mode](docs/core/env-api-key-defaults.md):** Lock down the entire UI. Force the engine to route through environment-injected API keys (`LLM_CONFIG_LOCKED`), guaranteeing secrets never touch the browser.
- **[Secure MCP OAuth](docs/core/oauth.md):** Seamless, auto-discovering OAuth 2.0 flows for connecting enterprise MCP servers safely.

### 👁️ Automation, Connectors, and Browser Control
Connect agents to real systems and communication channels.
- **[Google Workspace (GWS)](docs/core/google_workspace_integration.md):** Native CLI injection grants agents deterministic, scoped access to Gmail, Drive, Calendar, Docs, and Sheets.
- **[Vercel Agent-Browser](https://github.com/vercel-labs/agent-browser):** High-level browser automation engine used for complex web interactions, DOM analysis, and visual grounding.
- **[Browser System](docs/core/browser.md):** Covers browser session management, runtime limits, and browser integration patterns across providers.
- **[Bot Connectors](docs/core/bot_connector_system.md):** Expose your specialized agent swarms directly to Slack, Discord, or custom webhooks.

### 🤝 Human-in-the-Loop Operations
Keep operators involved when workflows need approval, intervention, or additional input.

- **[Human Feedback System](docs/workflow/human_feedback_system.md):** Agents can pause execution to request explicit approval, 2FA codes, or strategic guidance via real-time browser notifications or the visual dashboard.
- **[Slack Human Connector](docs/workflow/human_feedback_system.md#slack-configuration):** 
    - **Smart Delayed Notifications**: If a user doesn't respond in the UI within 2 minutes, the orchestrator automatically pings a configured Slack channel.
    - **Threaded Conversations**: Users can reply directly in the Slack thread to provide the required information, which is then fed back to the agent's context in real-time.
    - **Multi-User Collaboration**: Entire teams can monitor agent progress and intervene via Slack without ever opening the dashboard.

---

### 🧩 Supported Providers and Models

AgentForge is provider-agnostic, so you can combine different models across different workflow steps:

*   **OpenAI**: GPT-4o, GPT-4-turbo, and the O1 series.
*   **Anthropic**: Full support for the Claude 3.5 & 4 series (Sonnet, Opus, Haiku).
*   **Google Gemini**: Integration via Vertex AI or Google AI Studio (Flash, Pro, Ultra).
*   **AWS Bedrock**: Enterprise-grade access to Llama, Claude, and Mistral models.
*   **Azure AI Foundry**: Optimized support for Azure OpenAI, including specialized **Responses API** routing for agentic models like `gpt-5.2-codex`.
*   **MiniMax**: Support for high-performance MiniMax models.
*   **OpenRouter**: Unified access to 200+ open-source and frontier models with a single API key.

#### 🛠️ Local CLI Agents
Bring your existing CLI-based coding agents into the visual orchestrator via the **[MCP Bridge Layer](docs/core/mcp_bridge_layer.md)**:
*   **Claude Code**: Native integration with the `@anthropic-ai/claude-code` CLI.
*   **Gemini CLI**: Integration with the `@google-gemini/gemini-cli`.
*   **State Persistence**: Support for `--resume` functionality, allowing the visual orchestrator to maintain long-running coding sessions across CLI restarts.

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

Licensed under the MIT License - see the [LICENSE](LICENSE) file.

**Built Upon:**
- **[Model Context Protocol (MCP)](https://modelcontextprotocol.io/):** The universal standard for AI tool integration.
- **[LangChain Go](https://github.com/tmc/langchaingo):** High-performance LLM routing.
- **[React Flow](https://reactflow.dev/):** The industry standard for node-based visual editing.
