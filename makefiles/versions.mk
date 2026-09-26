# Canonical versions for build tools and images. No leading v here; versions
# read from go.mod already include it. No Go installation is needed to parse.
BUF_VERSION := 1.67.0
PROTOC_GEN_GO_GRPC_VERSION := 1.6.1
GOLANGCI_LINT_VERSION := 2.13.1
GOVULNCHECK_VERSION := 1.1.4
GRPC_HEALTH_PROBE_VERSION := 0.4.28
GRPCURL_VERSION := 1.9.1
ALPINE_VERSION := 3.20
NODE_VERSION := 20-alpine
TAILWIND_VERSION := 4.3.3
POSTGRES_VERSION := 16-alpine
PGVECTOR_VERSION := 0.8.5-pg16

GO_VERSION := $(shell awk '$$1 == "go" { print $$2; exit }' go.mod)
TEMPL_VERSION := $(shell awk '$$1 == "github.com/a-h/templ" { print $$2; exit }' go.mod)
PROTOC_GEN_GO_VERSION := $(shell awk '$$1 == "google.golang.org/protobuf" { print $$2; exit }' go.mod)

VERSION_VARS := GO_VERSION TEMPL_VERSION PROTOC_GEN_GO_VERSION BUF_VERSION \
	PROTOC_GEN_GO_GRPC_VERSION GOLANGCI_LINT_VERSION GOVULNCHECK_VERSION \
	GRPC_HEALTH_PROBE_VERSION GRPCURL_VERSION ALPINE_VERSION NODE_VERSION \
	TAILWIND_VERSION POSTGRES_VERSION PGVECTOR_VERSION
export $(VERSION_VARS)
