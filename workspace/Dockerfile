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
RUN apk add --no-cache ripgrep git patch python3 py3-pip util-linux jq

# Configure pip to allow system-wide packages (safe in containerized environment)
RUN mkdir -p /root/.config/pip && \
    echo "[global]" > /root/.config/pip/pip.conf && \
    echo "break-system-packages = true" >> /root/.config/pip/pip.conf

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/planner .

# Create docs and data directories
RUN mkdir -p /app/workspace-docs /app/data

# NOTE: Running as root to allow unshare with mount namespaces (requires CAP_SYS_ADMIN)
# The application itself uses BuildSafeEnvironment() to prevent secret leakage

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./planner", "server", "--port", "8080", "--docs-dir", "/app/workspace-docs"]
