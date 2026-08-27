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
COPY core/ ./core/
COPY cmd/ ./cmd/

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /core ./cmd/core/

RUN go install github.com/grpc-ecosystem/grpc-health-probe@v0.4.28 && \
    cp "$(go env GOPATH)/bin/grpc-health-probe" /grpc_health_probe && \
    chmod +x /grpc_health_probe


FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /core /core
COPY --from=builder /grpc_health_probe /grpc_health_probe

CMD ["/core"]
