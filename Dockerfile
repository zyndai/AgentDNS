# Dockerfile for Agent DNS (Hash + HTTP embedders only)
# For ONNX embedder support, use Dockerfile.onnx

# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy dependency files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build without CGO (Hash and HTTP embedders only, no ONNX)
RUN CGO_ENABLED=0 GOOS=linux go build -o /agentdns -ldflags="-s -w" ./cmd/agentdns

# Runtime stage — use alpine for minimal image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

COPY --from=builder /agentdns /usr/local/bin/agentdns
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Create data directory
RUN mkdir -p /data /config

# Default config location
ENV AGENTDNS_DATA_DIR=/data
ENV AGENTDNS_CONFIG=/config/config.toml

EXPOSE 8080 4001

VOLUME ["/data", "/config"]

ENTRYPOINT ["entrypoint.sh"]
CMD ["agentdns", "start", "--config", "/config/config.toml"]
