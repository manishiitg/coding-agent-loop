# Build stage
FROM golang:1.24-alpine AS builder

# Install ripgrep, gcc, and musl-dev for CGO support
RUN apk add --no-cache ripgrep gcc musl-dev git

# Set working directory
WORKDIR /app

# Copy go mod files first
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for SQLite
ENV CGO_ENABLED=1
RUN go build -o planner .

# Final stage
FROM alpine:latest

# Install ripgrep, git, patch, python3 for GitHub integration, diff patching, script execution, util-linux for filesystem isolation (unshare), and jq for JSON processing
# Also install nodejs and npm for agent-browser, chromium for headless browser automation
# gcompat provides glibc compatibility for agent-browser binary (which is compiled against glibc)
# build-base (gcc, g++, make) so venv pip installs (e.g. matplotlib from source) work in execute_shell_command
RUN apk add --no-cache ripgrep git patch python3 py3-pip util-linux jq nodejs npm chromium chromium-chromedriver gcompat \
    py3-matplotlib py3-pandas build-base python3-dev curl wget bash

# Install agent-browser globally for browser automation
# PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 prevents downloading glibc-linked Chromium binaries
# that crash on Alpine/musl. We use the system-installed chromium-browser instead via
# AGENT_BROWSER_EXECUTABLE_PATH (read by agent-browser's daemon).
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm install -g agent-browser && \
    rm -rf /root/.cache/ms-playwright /tmp/.cache/ms-playwright

# Set Chromium path for agent-browser (system chromium on Alpine)
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium-browser

# Configure pip: allow installs when Python is "externally managed" (PEP 668)
# Shell runs with HOME=/tmp so /root/.config is unused; use /etc/pip.conf so all pip invocations see it
RUN echo "[global]" > /etc/pip.conf && \
    echo "break-system-packages = true" >> /etc/pip.conf

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/planner .

# Create docs and data directories; empty workspace-docs structure (Downloads, Chats, Plans)
RUN mkdir -p /app/workspace-docs /app/data \
  /app/workspace-docs/Downloads /app/workspace-docs/Chats /app/workspace-docs/Plans

# NOTE: Running as root to allow unshare with mount namespaces (requires CAP_SYS_ADMIN)
# The application itself uses BuildSafeEnvironment() to prevent secret leakage

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./planner", "server", "--port", "8080", "--docs-dir", "/app/workspace-docs"]
