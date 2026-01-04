# Build stage
FROM golang:1.24-alpine AS builder

# Install ripgrep, gcc, and musl-dev for CGO support
RUN apk add --no-cache ripgrep gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Ensure go.sum is complete and up-to-date
RUN go mod tidy

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for SQLite
ENV CGO_ENABLED=1
RUN go build -o planner .

# Final stage
FROM alpine:latest

# Install ripgrep, git, patch, python3 for GitHub integration, diff patching, script execution, and util-linux for filesystem isolation (unshare)
RUN apk add --no-cache ripgrep git patch python3 py3-pip util-linux

# Create app user
RUN adduser -D -s /bin/sh appuser

# Configure pip to allow system-wide packages (safe in containerized environment)
# Create pip config directory for appuser
RUN mkdir -p /home/appuser/.config/pip && \
    echo "[global]" > /home/appuser/.config/pip/pip.conf && \
    echo "break-system-packages = true" >> /home/appuser/.config/pip/pip.conf && \
    chown -R appuser:appuser /home/appuser/.config

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/planner .

# Create docs and data directories
RUN mkdir -p /app/workspace-docs /app/data && chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./planner", "server", "--port", "8080", "--docs-dir", "/app/workspace-docs"]
