# Multi-stage Dockerfile for Carrier-Grade SIP Proxy
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod and sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build Binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o sip-proxy ./cmd/edge-proxy/main.go

# Production Image
FROM alpine:latest
RUN apk add --no-cache ca-certificates

WORKDIR /root/
COPY --from=builder /app/sip-proxy .

# Standard SIP Ports
EXPOSE 5060/udp
EXPOSE 5060/tcp

CMD ["./sip-proxy"]
