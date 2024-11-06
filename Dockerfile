# Build stage
FROM golang:1.22.4 AS builder
WORKDIR /build

# Add build arg for version
ARG VERSION
ENV VERSION=${VERSION:-dev}

# Install build dependencies
RUN sed -i 's/main$/main contrib/' /etc/apt/sources.list && \
    apt-get update && \
    apt-get install -y \
        gcc \
        pkg-config \
        upx \
    && rm -rf /var/lib/apt/lists/*

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -v -trimpath -ldflags="-s -w -X main.Version=${VERSION}" \
    -o infoscope ./cmd/infoscope && \
    upx --best --lzma infoscope

# Final stage
FROM ubuntu:22.04
WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

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

# Run binary
ENTRYPOINT ["/app/infoscope"]