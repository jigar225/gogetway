# Go Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o goget ./cmd/server/

# Build the worker binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o goget-worker cmd/worker/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S goget && \
    adduser -u 1000 -S goget -G goget

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/goget .
COPY --from=builder /app/goget-worker .

# Copy default config (compose usually mounts config.docker.yaml over this)
COPY --chown=goget:goget config/config.yaml ./config.yaml

# Copy pricing file
COPY --from=builder --chown=goget:goget /app/internal/core/config/model_prices_and_context_window.json ./internal/core/config/

# Change ownership
RUN chown -R goget:goget /app

# Use non-root user
USER goget

# Expose ports
EXPOSE 8080 8081 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["./goget"]
