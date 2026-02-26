# Premium Optimized Dockerfile for Railway.app
# Stage 1: Build
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache git ca-certificates build-base

# Copy dependency files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy the entire project source
COPY . .

# Run tidy to ensure everything is perfect
RUN go mod tidy

# Build the SIP Engine binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nextgen-sip ./cmd/edge-proxy/main.go

# Stage 2: Runtime
FROM alpine:latest
RUN apk add --no-cache ca-certificates

WORKDIR /root/

# Copy binary and web assets
COPY --from=builder /app/nextgen-sip .
COPY --from=builder /app/web ./web

# Expose Admin API Port (Standard for Railway)
EXPOSE 8080

# Expose SIP Ports (UDP & TCP)
EXPOSE 5060/udp
EXPOSE 5060/tcp

# Set Environment Variables
ENV PORT=8080
ENV SIP_PORT=5060
ENV SIP_PROTOCOL=udp

# Start the system
CMD ["./nextgen-sip"]
