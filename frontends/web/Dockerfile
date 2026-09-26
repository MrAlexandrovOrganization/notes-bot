# check=skip=InvalidDefaultArgInFrom

# Base image versions are required build args supplied by Make/Compose.
ARG GO_VERSION
ARG ALPINE_VERSION
ARG NODE_VERSION
FROM golang:${GO_VERSION}-alpine AS gobuilder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION
ARG BUF_VERSION
ARG TEMPL_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION} && \
    go install github.com/bufbuild/buf/cmd/buf@v${BUF_VERSION} && \
    go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

COPY buf.gen.yaml ./
COPY proto/ ./proto/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    buf generate

COPY internal/ ./internal/
COPY frontends/ ./frontends/
COPY cmd/ ./cmd/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    templ generate ./frontends/web/views


# Tailwind CLI needs Node — the standalone Bun-based binary is unreliable
# under some container runtimes (spurious "input file does not exist"
# errors). Runs in its own stage against just the generated views (which
# contain the Tailwind utility classes as literal strings) and input.css.
FROM node:${NODE_VERSION} AS twbuilder

WORKDIR /css
COPY --from=gobuilder /app/frontends/web/views ./views
COPY --from=gobuilder /app/frontends/web/webapp/static/input.css ./input.css
ARG TAILWIND_VERSION
RUN --mount=type=cache,target=/root/.npm \
    npm install --no-save @tailwindcss/cli@${TAILWIND_VERSION} && \
    ./node_modules/.bin/tailwindcss -i ./input.css -o ./output.css --minify


FROM gobuilder AS finalbuilder

COPY --from=twbuilder /css/output.css frontends/web/webapp/static/tailwind.css

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /web ./cmd/web/


FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 10001 app

COPY --from=finalbuilder /web /web

USER app

CMD ["/web"]
