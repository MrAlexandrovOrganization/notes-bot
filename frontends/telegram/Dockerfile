FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY proto/ ./proto/
COPY internal/ ./internal/
COPY frontends/ ./frontends/
COPY cmd/ ./cmd/

RUN go build -o /telegram ./cmd/telegram/


FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /telegram /telegram

CMD ["/telegram"]
