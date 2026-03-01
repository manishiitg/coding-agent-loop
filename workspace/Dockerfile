# Build stage — use Debian-based Go image so the binary links against glibc (matching the runtime stage)
FROM golang:1.24 AS builder

# Install ripgrep and gcc for CGO support
RUN apt-get update && apt-get install -y --no-install-recommends ripgrep gcc git && rm -rf /var/lib/apt/lists/*

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

# Final stage — Debian for full glibc support (needed by Camoufox Firefox binary)
FROM debian:bookworm-slim

# Install system packages (Debian equivalents of the previous Alpine packages)
# ripgrep, git, patch, python3, util-linux (unshare), jq, nodejs, npm, chromium, build tools, curl, wget, bash
RUN apt-get update && apt-get install -y --no-install-recommends \
    ripgrep git patch python3 python3-pip python3-venv python3-dev \
    util-linux jq chromium chromium-driver \
    python3-matplotlib python3-pandas \
    build-essential curl wget bash ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install agent-browser globally for browser automation
# PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 prevents downloading extra Chromium binaries
# We use the system-installed chromium instead via AGENT_BROWSER_EXECUTABLE_PATH
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm install -g agent-browser && \
    rm -rf /root/.cache/ms-playwright /tmp/.cache/ms-playwright

# Set Chromium path for agent-browser (system chromium on Debian)
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium

# Configure pip: allow installs when Python is "externally managed" (PEP 668)
RUN echo "[global]" > /etc/pip.conf && \
    echo "break-system-packages = true" >> /etc/pip.conf

# Install camoufox for stealth browser automation (anti-detect Firefox fork)
# On Debian/glibc this installs cleanly — no musl workarounds needed
RUN pip install camoufox && \
    python3 -m camoufox fetch

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
