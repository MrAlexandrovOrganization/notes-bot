FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY proto/ ./proto/
COPY internal/ ./internal/
COPY core/ ./core/
COPY cmd/ ./cmd/

RUN go build -o /core ./cmd/core/

RUN apk add --no-cache curl && \
    ARCH=$(uname -m) && \
    case "$ARCH" in \
        x86_64)        PROBE_ARCH=amd64 ;; \
        aarch64|arm64) PROBE_ARCH=arm64 ;; \
        *) echo "Unsupported arch: $ARCH" && exit 1 ;; \
    esac && \
    curl -fsSL -o /grpc_health_probe \
        "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.28/grpc_health_probe-linux-${PROBE_ARCH}" && \
    chmod +x /grpc_health_probe


FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /core /core
COPY --from=builder /grpc_health_probe /grpc_health_probe

CMD ["/core"]
