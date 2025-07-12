# Multi-stage Dockerfile for pubsub-sub-bench
# Stage 1: Build environment
FROM golang:1.24-alpine AS builder

# Install git for build-time Git variables and ca-certificates
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Get Git information for build
ARG GIT_SHA
ARG GIT_DIRTY
RUN if [ -z "$GIT_SHA" ]; then \
        GIT_SHA=$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
    fi && \
    if [ -z "$GIT_DIRTY" ]; then \
        GIT_DIRTY=$(git diff --no-ext-diff 2>/dev/null | wc -l || echo "0"); \
    fi && \
    echo "Building with GIT_SHA=${GIT_SHA}, GIT_DIRTY=${GIT_DIRTY}" && \
    CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-w -s -X 'main.GitSHA1=${GIT_SHA}' -X 'main.GitDirty=${GIT_DIRTY}'" \
        -a -installsuffix cgo \
        -o pubsub-sub-bench .

# Stage 2: Runtime environment
FROM alpine:latest

# Install ca-certificates for TLS connections to Redis
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/pubsub-sub-bench .

# Create output directory with proper permissions
RUN mkdir -p /app/output && \
    chown appuser:appgroup /app/pubsub-sub-bench /app/output && \
    chmod +x /app/pubsub-sub-bench && \
    chmod 755 /app/output

# Switch to non-root user
USER appuser

# Set working directory for output files
WORKDIR /app/output

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD /app/pubsub-sub-bench --help > /dev/null || exit 1

# Expose common Redis ports (for documentation purposes)
EXPOSE 6379 6380

# Set entrypoint
ENTRYPOINT ["/app/pubsub-sub-bench"]

# Default command (show help)
CMD ["--help"]
