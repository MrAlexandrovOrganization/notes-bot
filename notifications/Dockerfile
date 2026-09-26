# check=skip=InvalidDefaultArgInFrom

# Base image versions are required build args supplied by Make/Compose.
ARG GO_VERSION
ARG ALPINE_VERSION
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION
ARG BUF_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION} && \
    go install github.com/bufbuild/buf/cmd/buf@v${BUF_VERSION}

COPY buf.gen.yaml ./
COPY proto/ ./proto/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    buf generate

COPY internal/ ./internal/
COPY notifications/ ./notifications/
COPY cmd/ ./cmd/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /notifications ./cmd/notifications/

ARG GRPC_HEALTH_PROBE_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/grpc-ecosystem/grpc-health-probe@v${GRPC_HEALTH_PROBE_VERSION} && \
    cp "$(go env GOPATH)/bin/grpc-health-probe" /grpc_health_probe && \
    chmod +x /grpc_health_probe


FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 app

COPY --from=builder /notifications /notifications
COPY --from=builder /grpc_health_probe /grpc_health_probe

USER app

CMD ["/notifications"]
