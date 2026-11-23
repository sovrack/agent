# ==============================================================================
# Sovrack Outbound Tunnel Agent Dockerfile
# Ultra-lightweight multi-stage container build (~18MB)
# ==============================================================================
FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/sovrack-agent ./cmd/agent

# Final minimal runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates docker-cli curl bash tzdata

COPY --from=builder /bin/sovrack-agent /usr/local/bin/sovrack-agent

ENV SOVRACK_SERVER_URL="wss://app.sovrack.com/agent-tunnel"
ENV SOVRACK_DOCKER_HOST="unix:///var/run/docker.sock"

ENTRYPOINT ["/usr/local/bin/sovrack-agent"]
