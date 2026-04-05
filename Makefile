DOCKER_COMPOSE = docker compose

# Canonical source of whisper.proto (shared with backends/transcriber).
# For remote fetch (e.g. in CI without access to the backend repo):
#   make proto-whisper WHISPER_PROTO_SRC=https://raw.githubusercontent.com/org/transcriber/main/proto/whisper.proto
WHISPER_PROTO_SRC ?= ../../backends/transcriber/proto/whisper.proto

# Install all dev tools: buf + Go protoc plugins.
# buf install: https://buf.build/docs/installation
#   macOS: brew install bufbuild/buf/buf
install:
	go install github.com/bufbuild/buf/cmd/buf@v1.67.0
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1

# All Go unit test packages (no integration)
GO_UNIT_PKGS = ./core/... ./core/features/... ./notifications/... \
               ./frontends/telegram/tghandlers/... \
               ./frontends/telegram/tgkeyboards/... \
               ./frontends/telegram/tgstates/...

# All packages to instrument for coverage
TELEGRAM_COVERPKGS = notes-bot/frontends/telegram/tghandlers,notes-bot/frontends/telegram/tgkeyboards,notes-bot/frontends/telegram/tgstates
GO_COVERPKGS_UNIT = notes-bot/core,notes-bot/core/features,notes-bot/notifications,$(TELEGRAM_COVERPKGS)
GO_COVERPKGS_ALL  = notes-bot/core,notes-bot/core/features,notes-bot/notifications,$(TELEGRAM_COVERPKGS)

test-go:
	go test $(GO_UNIT_PKGS)

test-go-cover:
	go test -coverprofile=coverage.out -coverpkg=$(GO_COVERPKGS_UNIT) $(GO_UNIT_PKGS)
	go tool cover -func=coverage.out
	@rm -f coverage.out

test-go-cover-html:
	go test -coverprofile=coverage.out -coverpkg=$(GO_COVERPKGS_UNIT) $(GO_UNIT_PKGS)
	go tool cover -html=coverage.out
	@rm -f coverage.out

cover:
	go test -coverprofile=unit.out -coverpkg=$(GO_COVERPKGS_ALL) $(GO_UNIT_PKGS)
	go test -coverprofile=integration.out -coverpkg=notes-bot/core,notes-bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -func=combined.out
	@rm -f unit.out integration.out combined.out

cover-html:
	go test -coverprofile=unit.out -coverpkg=$(GO_COVERPKGS_ALL) $(GO_UNIT_PKGS)
	go test -coverprofile=integration.out -coverpkg=notes-bot/core,notes-bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	@mkdir -p htmlcov
	go tool cover -html=combined.out -o htmlcov/go_coverage.html
	@rm -f unit.out integration.out combined.out
	@echo "Coverage report saved to htmlcov/go_coverage.html"
	open htmlcov/go_coverage.html

test-integration:
	go test ./integration/... -v

test-notifications:
	go test ./notifications/... -v

test:
	go test -coverprofile=coverage.out -coverpkg=$(GO_COVERPKGS_UNIT) $(GO_UNIT_PKGS)
	go tool cover -func=coverage.out
	@rm -f coverage.out

build-notifications:
	$(DOCKER_COMPOSE) build notifications

build-telegram:
	$(DOCKER_COMPOSE) build telegram

format:
	gofmt -w ./notifications/ ./frontends/telegram/ ./cmd/

clean:
	find . -type f -name '*.pyc' -delete
	find . -type d -name '__pycache__' -exec rm -rf {} +

up:
	$(DOCKER_COMPOSE) up --build -d
	$(DOCKER_COMPOSE) logs -f

up-ci:
	$(DOCKER_COMPOSE) up --build -d

deploy:
	$(DOCKER_COMPOSE) build --no-cache
	$(DOCKER_COMPOSE) up -d

down:
	$(DOCKER_COMPOSE) down

logs:
	$(DOCKER_COMPOSE) logs -f

restart:
	$(DOCKER_COMPOSE) up --build --force-recreate -d
	$(DOCKER_COMPOSE) logs -f

build-core:
	$(DOCKER_COMPOSE) build core

docker-clean:
	$(DOCKER_COMPOSE) down --rmi all --remove-orphans
	docker system prune -f

# Sync whisper.proto from the canonical source (backends/transcriber), then regenerate.
proto:
	@echo "Syncing whisper.proto from $(WHISPER_PROTO_SRC)..."
	@if echo "$(WHISPER_PROTO_SRC)" | grep -qE "^https?://"; then \
		curl -sSfL "$(WHISPER_PROTO_SRC)" -o proto/whisper/whisper.proto; \
	else \
		cp "$(WHISPER_PROTO_SRC)" proto/whisper/whisper.proto; \
	fi
	@sed -i.bak 's|option go_package = "[^"]*";|option go_package = "notes-bot/proto/whisper";|' proto/whisper/whisper.proto
	@rm -f proto/whisper/whisper.proto.bak
	buf generate

proto-lint:
	buf lint proto

.PHONY: install test-go test-go-cover test-go-cover-html cover cover-html test-integration test-notifications test clean build-core build-notifications build-telegram up up-ci deploy down logs restart docker-clean proto proto-lint format
