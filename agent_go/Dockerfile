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

# Copy workspace module first (for replace directive)
COPY workspace/go.mod workspace/go.sum ./workspace/
COPY workspace/ ./workspace/

# Copy agent_go module
COPY agent_go/go.mod agent_go/go.sum ./agent_go/

# Download dependencies
WORKDIR /app/agent_go
RUN go mod download

# Copy agent source code
COPY agent_go/ ./

# Minimal go.work so workspace module resolves (handlers import "workspace/models" etc.)
RUN printf 'go 1.24.4\nuse (\n\t./agent_go\n\t./workspace\n)\n' > /app/go.work
ENV GOWORK=/app/go.work
# Build from /app so go.work is found; output binary to agent_go dir (glibc-linked for Debian runtime)
RUN cd /app && CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /app/agent_go/mcp-agent ./agent_go

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

# Install agent-browser globally for browser automation
RUN npm install -g agent-browser

# Set Chromium path for Playwright (used by agent-browser)
ENV PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium

# Install uv (uvx for Python MCP servers)
RUN pip3 install --break-system-packages uv

# Verify npx and docker are available
RUN which npx && which docker && npm --version && npx --version && docker --version

# Verify ripgrep and jq
RUN which rg && which jq && rg --version && jq --version

# Create non-root user BEFORE pre-warming so caches land in the correct home directory.
# At runtime the container runs as appuser, so caches must be in /home/appuser/.cache/
RUN groupadd -g 1001 appgroup && useradd -m -u 1001 -g appgroup appuser

# =============================================================================
# Pre-warm MCP server packages at build time as appuser so the uv/npm caches
# are in the right home directory and reused at runtime (no re-install on pod start).
# =============================================================================
USER appuser

# Pre-warm Python (uvx) MCP servers — caches into /home/appuser/.cache/uv/
RUN uvx awslabs.aws-api-mcp-server@latest --help 2>/dev/null || true \
    && uvx awslabs.aws-pricing-mcp-server@latest --help 2>/dev/null || true \
    && uvx awslabs.cost-explorer-mcp-server@latest --help 2>/dev/null || true \
    && uvx awslabs.cloudwatch-mcp-server@latest --help 2>/dev/null || true \
    && uvx awslabs.billing-cost-management-mcp-server@latest --help 2>/dev/null || true \
    && uvx awslabs.aws-documentation-mcp-server@latest --help 2>/dev/null || true

# Pre-warm Node (npx) MCP servers — caches into /home/appuser/.npm/
RUN npx --yes @sylphx/pdf-reader-mcp --help 2>/dev/null || true \
    && npx --yes mcp-remote --help 2>/dev/null || true \
    && npx --yes @negokaz/excel-mcp-server --help 2>/dev/null || true \
    && npx --yes kubernetes-mcp-server@latest --help 2>/dev/null || true \
    && npx --yes @leval/mcp-grafana --help 2>/dev/null || true

# Pre-warm Python (uv run) MCP servers
RUN uv run --with mcp-clickhouse --python 3.13 mcp-clickhouse --help 2>/dev/null || true

# Switch back to root for remaining setup steps (copy binary, chown, etc.)
USER root

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/agent_go/mcp-agent .

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
CMD ["./mcp-agent", "server", "--host", "0.0.0.0", "--port", "8000", "--log-file", "/app/logs/server_debug.log", "--log-level", "debug", "--mcp-config", "configs/mcp_servers_simple.json"]
