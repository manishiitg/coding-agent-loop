# =============================================================================
# Why Debian (not Alpine) for the agent image:
# - Alpine uses musl libc; many MCP servers (e.g. mcp-clickhouse/chdb) need glibc
#   and libintl (gettext). On Alpine this required workarounds and still broke.
# - docker-ce-cli is easy to install from Docker's apt repo on Debian; on Alpine
#   it lives in the community repo and needs extra setup.
# - Single glibc-based base (Debian) avoids native extension and symbol issues
#   for Python/Node MCPs. Builder is also Debian so the Go binary is glibc-linked.
# =============================================================================

# Build stage - Debian so the binary links against glibc and runs on the Debian runtime
FROM golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates gcc libc6-dev && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy all module definitions first for dependency caching
COPY workspace/go.mod workspace/go.sum ./workspace/
COPY agent_go/go.mod agent_go/go.sum ./agent_go/

# go.work so workspace module resolves locally (agent_go depends on workspace)
RUN printf 'go 1.24.4\nuse (\n\t./agent_go\n\t./workspace\n)\n' > /app/go.work
ENV GOWORK=/app/go.work

# Download dependencies (retry up to 3 times — proxy.golang.org can be flaky)
# mcpagent and multi-llm-provider-go come from GitHub via go.mod tags
WORKDIR /app/agent_go
RUN for i in 1 2 3; do go mod download && break || echo "Retry $i..."; sleep 5; done

# Copy source code
WORKDIR /app
COPY workspace/ ./workspace/
COPY agent_go/ ./agent_go/
# Build from /app so go.work is found; output binary to agent_go dir (glibc-linked for Debian runtime)
RUN cd /app && CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /app/agent_go/mcp-agent ./agent_go

# Build mcpbridge binary (Gemini CLI MCP bridge - proxies tool calls to our API)
RUN cd /app && CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/agent_go/mcpbridge github.com/manishiitg/mcpagent/cmd/mcpbridge

# Runtime stage - Debian bookworm-slim (glibc, good package availability)
FROM debian:bookworm-slim

# Base + MCP runtime: curl/wget/bash for scripts; jq/ripgrep for tools; Python+uv for
# Python MCPs (e.g. uvx run mcp-clickhouse); gettext-base for libintl (chdb/native deps).
# gnupg for adding Docker's apt repo.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    bash \
    jq \
    ripgrep \
    python3 \
    python3-pip \
    python3-venv \
    gettext-base \
    gnupg \
    && curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && apt-get install -y --no-install-recommends chromium \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
# docker-ce-cli: MCP configs can use "command": "docker", "args": ["run", "-i", "--rm", "mcp/sonarqube"] (mount /var/run/docker.sock in deployment).
# nodejs 20+: required for npx MCPs (mcp-remote/undici, @sylphx/pdf-reader-mcp, etc.). chromium: agent-browser/Playwright. gcc/libc6-dev: CGO and native Python deps.

# Install Google Workspace CLI for GWS integration
RUN npm install -g @googleworkspace/cli

# Install uv (uvx for Python MCP servers) and pypdf (for read_pdf tool)
RUN pip3 install --break-system-packages uv pypdf

# Verify npx and docker are available
RUN which npx && which docker && npm --version && npx --version && docker --version

# Verify ripgrep and jq
RUN which rg && which jq && rg --version && jq --version

# Create non-root user. Runtime caches (uvx/npx) land in /home/appuser/.cache/
RUN groupadd -g 1001 appgroup && useradd -m -u 1001 -g appgroup appuser

# MCP server packages (uvx/npx) are installed lazily on first connection at runtime.
# The ConnectTimeout (15 min) in mcpclient/client.go handles first-run install time.

# Set working directory
WORKDIR /app

# Copy the binaries from builder stage
COPY --from=builder /app/agent_go/mcp-agent .
COPY --from=builder /app/agent_go/mcpbridge /usr/local/bin/mcpbridge

# Copy Python scripts (PDF extraction, etc.)
COPY agent_go/scripts/extract_pdf_text.py /app/scripts/

# Copy Go from builder for code execution mode (run Go snippets)
COPY --from=builder /usr/local/go /usr/local/go
ENV GOROOT=/usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
RUN go version

# Copy configuration files
COPY --from=builder /app/agent_go/configs ./configs

# Copy database migration files (required for schema creation on startup)
COPY --from=builder /app/agent_go/pkg/database/migrations ./pkg/database/migrations
COPY --from=builder /app/agent_go/pkg/database/migrations_postgres ./pkg/database/migrations_postgres

# Create reports and logs directories
RUN mkdir -p reports logs && chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8000

# Set default command
CMD ["./mcp-agent", "server", "--host", "0.0.0.0", "--port", "8000", "--log-file", "/app/logs/server_debug.log", "--log-level", "debug", "--mcp-config", "configs/mcp_servers_docker.json"]
