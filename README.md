# 🚀 MCP Agent - Multi-Server AI Orchestrator

![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/manishiitg/mcp-agent-builder-go)


[![Security Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Secret%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Dependency Scan](https://github.com/manishiitg/mcp-agent-builder-go/workflows/Dependency%20Scan/badge.svg)](https://github.com/manishiitg/mcp-agent-builder-go/actions)
[![Go Version](https://img.shields.io/badge/Go-1.24.4-blue.svg)](https://golang.org/)
[![React](https://img.shields.io/badge/React-19.1.1-blue.svg)](https://reactjs.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A sophisticated **Go-based MCP (Model Context Protocol) Agent** featuring a complete **3-agent orchestrator system** that combines intelligent planning, tool execution, and fact-checking validation for enterprise-grade AI workflows. Includes a modern React frontend and comprehensive security scanning.

## 🎯 **What is MCP Agent?**

MCP Agent is a production-ready AI orchestrator that connects to **12+ MCP servers** across multiple protocols (HTTP, SSE, stdio) to provide intelligent automation across AWS, GitHub, Kubernetes, databases, monitoring tools, and more. It features both **Simple** and **ReAct** agent modes with comprehensive observability and security scanning.

## 🏗️ **Workflow Orchestration**

MCP Agent provides sophisticated workflow orchestration capabilities for complex AI automation:

### **📚 Available Workflows**

- **[Todo Creation Human Workflow](docs/todo_creation_human_workflow.md)**: Multi-agent system for creating validated todo lists via step-by-step execution, learning, and synthesis. Features human-in-the-loop control, conditional branching, loop execution, and comprehensive learning system.

- **[Code Execution Agent](docs/code_execution_agent.md)**: Specialized agent for executing Python code with security sandboxing and comprehensive error handling.

- **[Standard Tool-Use Agent](docs/tool_use_agent.md)**: The default agent mode where the LLM interacts with the system by invoking tools directly through the LLM provider's native tool calling capability.

- **[Smart Routing](docs/smart_routing.md)**: Advanced optimization that dynamically filters tools based on conversation context to reduce token usage.

- **[LLM Resilience](docs/llm_resilience.md)**: Comprehensive system for handling API errors, rate limits, and context window exhaustion with multi-phase fallbacks.

- **[Large Tool Output Handling](docs/large_output_handling.md)**: Automatic system for handling tool outputs that exceed context window limits by saving to files and providing specialized query tools.

- **[MCP Cache System](docs/mcp_cache_system.md)**: Multi-layer caching system that reduces MCP server connection times by 60-85% through intelligent caching of tool definitions and server metadata.


See the [docs/](docs/) folder for detailed documentation on each workflow and agent.

## 🚀 **Quick Start**

### **Prerequisites**
- Go 1.24.4+
- Node.js 20+
- Docker & Docker Compose (optional)

### **1. Clone the Repository**
```bash
git clone https://github.com/manishiitg/mcp-agent-builder-go.git
cd mcp-agent-builder-go
```

### **2. Environment Setup**
```bash
# Copy environment template
cp agent_go/env.example agent_go/.env

# Edit with your API keys
nano agent_go/.env
```

**Required Environment Variables:**
```bash
# OpenAI
OPENAI_API_KEY=your_openai_key

# AWS Bedrock (optional)
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# Langfuse (optional)
LANGFUSE_PUBLIC_KEY=your_public_key
LANGFUSE_SECRET_KEY=your_secret_key
TRACING_PROVIDER=langfuse
```

### **3. Build and Run**

#### **Option A: Docker Compose (Recommended)**
```bash
# Start all services
docker-compose up -d

# Access the application
open http://localhost:5173  # Frontend
open http://localhost:8000  # API
```

#### **Option B: Manual Build**
```bash
# Build Go agent
cd agent_go
go build -o ../bin/orchestrator .

# Install frontend dependencies
cd ../frontend
npm install

# Start services
npm run dev &  # Frontend on :5173
../bin/orchestrator server &  # API on :8000
```

### **4. Test the Agent**
```bash
# Test with AWS cost analysis
cd agent_go
../bin/orchestrator test agent --comprehensive-aws --provider bedrock

# Test all MCP servers
../bin/orchestrator test aws-test --config configs/mcp_server_actual.json
```

## 📖 **Usage Examples**

### **Simple Agent Mode**
```bash
# Direct tool usage without explicit reasoning
../bin/orchestrator agent --simple --provider bedrock --query "What's the status of my AWS EC2 instances?"
```

### **ReAct Agent Mode**
```bash
# Step-by-step reasoning with tool integration
../bin/orchestrator agent --react --provider openai --query "Analyze my GitHub repository for security vulnerabilities and provide recommendations"
```

### **3-Agent Orchestrator**
```bash
# Complete planning → execution → validation workflow
../bin/orchestrator orchestrator --query "Create a comprehensive security assessment of my AWS infrastructure"
```

### **External Package Usage**
The external package provides a clean Go API for integrating the MCP agent into your applications. See the [External Package Documentation](agent_go/pkg/external/README.md) for detailed usage examples.

## 🔧 **Configuration**

### **MCP Server Configuration**
Edit `agent_go/configs/mcp_server_actual.json` to configure your MCP servers. See the [Configuration Guide](agent_go/configs/) for detailed examples.

### **Agent Configuration**
The agent supports flexible configuration through the external package. See the [External Package Documentation](agent_go/pkg/external/README.md) for detailed configuration options and examples.

## 🧪 **Testing**

### **Comprehensive Testing Suite**
```bash
# Test all MCP servers
../bin/orchestrator test aws-test --config configs/mcp_server_actual.json

# Test agent modes
../bin/orchestrator test agent --simple --provider bedrock
../bin/orchestrator test agent --react --provider openai

# Test external SSE servers
./test_external_sse.sh

# Test complex AWS cost analysis
./test_single_observer.sh
./test_polling_api.sh
```

### **Security Testing**
```bash
# Run gitleaks scan
./scripts/scan-secrets.sh

# Test pre-commit hook
git add .
git commit -m "Test commit"
```

## 🔒 **Security Features**

### **Automated Secret Scanning**
- **Gitleaks Integration**: Pre-commit hooks prevent secret leaks
- **GitHub Actions**: Continuous security monitoring
- **Custom Rules**: Project-specific secret detection patterns
- **False Positive Handling**: Optimized for Go and Node.js projects

### **Dependency Security**
- **NPM Audit**: Frontend dependency vulnerability scanning
- **Go Vulnerability Check**: Backend dependency scanning
- **Dependabot**: Automated security updates
- **SARIF Reporting**: GitHub Security tab integration

### **Security Policies**
- **Responsible Disclosure**: Clear security reporting process
- **Issue Templates**: Structured security vulnerability reporting
- **Pull Request Templates**: Security checklist integration

## 📊 **Monitoring & Observability**

### **Langfuse Integration**
- **Complete Tracing**: All agent activities traced
- **Token Usage**: Accurate cost monitoring
- **Performance Metrics**: Real-time performance monitoring
- **Dashboard Access**: https://us.cloud.langfuse.com

### **Event Architecture**
- **System Events**: `system_prompt`, `user_message`
- **LLM Events**: `llm_generation_start`, `llm_generation_end`, `token_usage`
- **Tool Events**: `tool_call_start`, `tool_call_end`, `tool_call_error`
- **Completion Events**: `conversation_end`, `agent_end`

## 🐳 **Docker Support**

### **Full Stack Deployment**
```bash
# Start all services
docker-compose up -d

# Services included:
# - Frontend (React): http://localhost:5173
# - API (Go): http://localhost:8000
# - Planner API: http://localhost:8081
# - Qdrant Vector DB: http://localhost:6333
```

### **Individual Services**
```bash
# Build Go agent
docker build -t mcp-agent ./agent_go

# Build frontend
docker build -t mcp-frontend ./frontend

# Build planner
docker build -t mcp-planner ./planner
```

## 📁 **Project Structure**

```
mcp-agent-builder-go/
├── agent_go/                 # Go-based MCP Agent
│   ├── pkg/
│   │   ├── mcpagent/        # Core agent implementation
│   │   ├── mcpclient/       # MCP client layer
│   │   └── external/        # External package API
│   ├── cmd/                 # CLI commands
│   ├── configs/             # MCP server configurations
│   └── internal/            # Internal packages
├── frontend/                 # React frontend
│   ├── src/                 # React components
│   └── public/              # Static assets
├── planner/                  # Planning agent
├── memory/                   # Memory/vector database
├── scripts/                  # Utility scripts
├── .github/workflows/        # GitHub Actions
└── docker-compose.yml       # Docker services
```

## 🤝 **Contributing**

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

### **Development Setup**
```bash
# Install pre-commit hooks
./scripts/install-git-hooks.sh

# Run tests
cd agent_go && go test ./...
cd frontend && npm test

# Run security scan
./scripts/scan-secrets.sh
```

### **Security Reporting**
If you discover a security vulnerability, please report it responsibly:
1. **Public Issues**: Use the [Security Vulnerability Template](.github/ISSUE_TEMPLATE/security-vulnerability.md)
2. **Private Reporting**: See [SECURITY.md](SECURITY.md) for private reporting methods

## 📄 **License**

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 **Acknowledgments**

- **MCP Protocol**: Built on the [Model Context Protocol](https://modelcontextprotocol.io/)
- **LangChain Go**: Powered by [LangChain Go](https://github.com/tmc/langchaingo)
- **React**: Modern frontend with [React 19](https://reactjs.org/)
- **Gitleaks**: Security scanning with [Gitleaks](https://github.com/gitleaks/gitleaks)

## 📞 **Support**

- **Issues**: [GitHub Issues](https://github.com/manishiitg/mcp-agent-builder-go/issues)
- **Discussions**: [GitHub Discussions](https://github.com/manishiitg/mcp-agent-builder-go/discussions)
- **Security**: [SECURITY.md](SECURITY.md)

---

**Made with ❤️ for the AI community**
