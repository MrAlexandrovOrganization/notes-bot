FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1 && \
    go install github.com/bufbuild/buf/cmd/buf@v1.67.0

COPY buf.gen.yaml ./
COPY proto/ ./proto/
RUN buf generate

COPY internal/ ./internal/
COPY search/ ./search/
COPY cmd/ ./cmd/

RUN go build -o /search-bin ./cmd/search/

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


FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /search-bin /search
COPY --from=builder /grpc_health_probe /grpc_health_probe

CMD ["/search"]
