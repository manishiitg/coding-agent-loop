# 🚀 MCP Agent Builder - Enterprise AI Orchestrator

![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/manishiitg/mcp-agent-builder-go)
[![Security Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Secret%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Dependency Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Dependency%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Go Version](https://img.shields.io/badge/Go-1.24.4-blue.svg)](https://golang.org/)
[![React](https://img.shields.io/badge/React-19.1.1-blue.svg)](https://reactjs.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**MCP Agent Builder** is a next-generation visual orchestration platform for building, simulating, and deploying **multi-agent swarms**. Built on a high-performance Go backend and a React Flow canvas, it transforms raw LLM capabilities into deterministic, scalable, and secure enterprise workflows using the Model Context Protocol (MCP).

Stop wrestling with fragile chat scripts. Start visually engineering **self-optimizing cognitive architectures**.

---

## ⚡ The Platform: Visual Agentic Programming

At the core of MCP Agent Builder is the **[Workflow Orchestrator](docs/workflow_orchestrator.md)**—a directed acyclic graph (DAG) engine managed entirely through a rich **[React Flow Canvas](docs/react_flow_workflow_canvas.md)**. 

Design complex business processes visually. The orchestrator drives a 7-phase execution pipeline (Extraction, Planning, Execution, Anonymization, Improvement, Alignment, Optimization) powered by 13 specialized node types.

### 🧠 Self-Optimizing Cognitive Architecture
Move beyond static prompts. Our platform features an **"Explore vs. Exploit" Learning Engine**. 
- **[Validation & Learning](docs/learnings_and_validation_architecture.md):** As your agents execute workflows, the system tracks trajectory success rates. Once a pattern stabilizes, the orchestrator automatically locks the step and cascades to faster, more cost-effective LLMs.
- **[Swarm Delegation](docs/sub_agent_delegation.md):** Empower your primary agent to dynamically spawn independent sub-agents, parallelizing complex research, coding, or data extraction tasks across a distributed swarm.
- **[Task Orchestration](docs/todo-task-step-type.md):** Intelligent sub-task routing that manages state, dependencies, and context windows automatically.

### 🛡️ Enterprise-Grade Security & Guardrails
Deploy with confidence using deterministic controls designed for strict compliance environments.
- **[Zero-Trust Workspace (FolderGuard)](docs/multi_user_authentication.md):** Strict per-user filesystem isolation utilizing Linux namespaces. Agents operate in sandboxed environments, preventing cross-tenant data contamination.
- **[Restricted Configuration Mode](docs/env-api-key-defaults.md):** Lock down the entire UI. Force the engine to route through environment-injected API keys (`LLM_CONFIG_LOCKED`), guaranteeing secrets never touch the browser.
- **[Secure MCP OAuth](docs/oauth.md):** Seamless, auto-discovering OAuth 2.0 flows for connecting enterprise MCP servers safely.

### 👁️ Omni-Channel Automation & Browser Stealth
Interact with the real world, safely and reliably.
- **[Camoufox Stealth Integration](docs/camoufox_stealth_browser.md):** Native integration with Camoufox, providing agents with anti-detect browser automation capable of bypassing the most aggressive enterprise bot-protections.
- **[Local CDP Bridging](docs/cdp_local_browser.md):** Connect cloud agents directly to your local Chrome instance via CDP for real-time monitoring and session hijacking.
- **[Bot Connectors](docs/bot_connector_system.md):** Expose your specialized agent swarms directly to Slack, Discord, or custom webhooks.

### 🤝 Human-AI Symbiosis
Keep humans in the loop for critical decision-making.
- **[Interactive Feedback System](docs/human_feedback_system.md):** Agents can pause execution to request explicit approval, 2FA codes, or strategic guidance via browser notifications or delayed Slack alerts before taking destructive actions.

### 🧩 Supported Cognitive Engines (LLM Providers)

MCP Agent Builder is provider-agnostic, allowing you to switch between the world's most powerful models or use them in combination across different workflow steps:

*   **OpenAI**: GPT-4o, GPT-4-turbo, and the O1 series.
*   **Anthropic**: Full support for the Claude 3.5 & 4 series (Sonnet, Opus, Haiku).
*   **Google Gemini**: Integration via Vertex AI or Google AI Studio (Flash, Pro, Ultra).
*   **AWS Bedrock**: Enterprise-grade access to Llama, Claude, and Mistral models.
*   **Azure AI Foundry**: Optimized support for Azure OpenAI, including specialized **Responses API** routing for agentic models like `gpt-5.2-codex`.
*   **MiniMax**: Support for high-performance MiniMax models.
*   **OpenRouter**: Unified access to 200+ open-source and frontier models with a single API key.

#### 🛠️ Local CLI Agents (Coding Plans)
Seamlessly bridge your existing CLI-based coding agents into the visual orchestrator:
*   **Claude Code**: Native integration with the `@anthropic-ai/claude-code` CLI.
*   **Gemini CLI**: Integration with the `@google-gemini/gemini-cli`.
*   **State Persistence**: Support for `--resume` functionality, allowing the visual orchestrator to maintain long-running coding sessions across CLI restarts.

---

## 🚀 Quick Start (Local Development)

### 1. Initialize Workspace
```bash
git clone https://github.com/manishiitg/mcp-agent-builder-go.git
cd mcp-agent-builder-go
cp agent_go/env.example agent_go/.env
```

### 2. Configure Cognitive Engines (`agent_go/.env`)
```bash
# Power your swarms (Configure at least one)
OPENAI_API_KEY=your_openai_key
ANTHROPIC_API_KEY=your_anthropic_key

# Optional: Enable Enterprise Lockdown Mode
LLM_CONFIG_LOCKED=true
SUPPORTED_LLM_PROVIDERS=openai,anthropic
```

### 3. Ignite the Platform
```bash
# Spin up the Visual Canvas, Execution API, Planner, and Vector DB
docker-compose up -d

# Open the command center
open http://localhost:5173
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
