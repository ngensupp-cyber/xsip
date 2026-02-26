# Optimized Dockerfile for Railway.app Deployment
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go.mod and go.sum (if exists)
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code and web assets
COPY . .

# Build Binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nextgen-sip ./cmd/edge-proxy/main.go

# Production Environment
FROM alpine:latest
RUN apk add --no-cache ca-certificates

WORKDIR /root/
COPY --from=builder /app/nextgen-sip .
COPY --from=builder /app/web ./web

# Set up environment for high-performance
ENV PORT=8080
ENV SIP_PORT=5060
ENV SIP_PROTOCOL=udp

# Open ports
EXPOSE 8080
EXPOSE 5060/udp
EXPOSE 5060/tcp

CMD ["./nextgen-sip"]
