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
COPY frontends/ ./frontends/
COPY cmd/ ./cmd/

RUN go build -o /telegram ./cmd/telegram/


FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /telegram /telegram

CMD ["/telegram"]
