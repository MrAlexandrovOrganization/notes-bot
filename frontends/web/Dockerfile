FROM golang:1.26-alpine AS gobuilder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1 && \
    go install github.com/bufbuild/buf/cmd/buf@v1.67.0 && \
    go install github.com/a-h/templ/cmd/templ@v0.3.1020

COPY buf.gen.yaml ./
COPY proto/ ./proto/
RUN buf generate

COPY internal/ ./internal/
COPY frontends/ ./frontends/
COPY cmd/ ./cmd/

RUN templ generate ./frontends/web/views


# Tailwind CLI needs Node — the standalone Bun-based binary is unreliable
# under some container runtimes (spurious "input file does not exist"
# errors). Runs in its own stage against just the generated views (which
# contain the Tailwind utility classes as literal strings) and input.css.
FROM node:20-alpine AS twbuilder

WORKDIR /css
COPY --from=gobuilder /app/frontends/web/views ./views
COPY --from=gobuilder /app/frontends/web/webapp/static/input.css ./input.css
RUN npm install --no-save @tailwindcss/cli@4.3.3 && \
    npx @tailwindcss/cli@4.3.3 -i ./input.css -o ./output.css --minify


FROM gobuilder AS finalbuilder

COPY --from=twbuilder /css/output.css frontends/web/webapp/static/tailwind.css

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /web ./cmd/web/


FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 10001 app

COPY --from=finalbuilder /web /web

USER app

CMD ["/web"]
