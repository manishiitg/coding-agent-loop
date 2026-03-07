# Build stage
FROM golang:1.24-alpine AS builder

# Install ripgrep and gcc/musl-dev for CGO support
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
FROM alpine:3.20

# Install system packages
RUN apk add --no-cache \
    ripgrep git patch py3-pip python3 python3-dev \
    util-linux jq chromium chromium-chromedriver \
    py3-matplotlib py3-pandas \
    build-base curl wget bash ca-certificates gcompat nodejs npm

# Install agent-browser globally for browser automation
# PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 prevents downloading extra Chromium binaries
# We use the system-installed chromium instead via AGENT_BROWSER_EXECUTABLE_PATH
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm install -g agent-browser @googleworkspace/cli && \
    rm -rf /root/.cache/ms-playwright /tmp/.cache/ms-playwright

# Set Chromium path for agent-browser (system chromium on Alpine)
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium

# Configure pip: allow installs when Python is "externally managed" (PEP 668)
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
