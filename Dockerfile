# Build stage
FROM golang:1.22.4-bullseye AS builder
WORKDIR /build

# Add build arg for version
ARG VERSION
ENV VERSION=${VERSION:-dev}

# Install build dependencies
RUN apt-get update && \
    apt-get install -y \
        gcc \
        pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -v -trimpath -ldflags="-s -w -X main.Version=${VERSION}" \
    -o infoscope ./cmd/infoscope

# Final stage
FROM gcr.io/distroless/base-debian12
WORKDIR /app

# ca-certificates and tzdata are expected to be in the distroless base image or handled differently.
# No apt-get needed.

# Copy binary from builder
COPY --from=builder /build/infoscope /app/infoscope

# Create necessary directories
RUN mkdir -p /app/data /app/web

# Set default environment variables
ENV INFOSCOPE_PORT=8080 \
    INFOSCOPE_DB_PATH=/app/data/infoscope.db \
    INFOSCOPE_DATA_PATH=/app/data \
    INFOSCOPE_WEB_PATH=/app/web

# Expose default port
EXPOSE 8080

# Define volumes for persistence
VOLUME ["/app/data", "/app/web"]

# Run as non-root user
USER 65532:65532

# Healthcheck
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/infoscope", "-healthcheck"]

# Run binary
ENTRYPOINT ["/app/infoscope"]