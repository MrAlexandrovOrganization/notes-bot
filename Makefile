DOCKER_COMPOSE = docker compose

# For mac: brew install go@1.25
# For CI: Go is installed via actions/setup-go
install:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1

# All Go unit test packages (no integration)
GO_UNIT_PKGS = ./core/... ./core/features/... ./notifications/... \
               ./frontends/telegram/tghandlers/... \
               ./frontends/telegram/tgkeyboards/... \
               ./frontends/telegram/tgstates/...

# All packages to instrument for coverage
TELEGRAM_COVERPKGS = notes_bot/frontends/telegram/tghandlers,notes_bot/frontends/telegram/tgkeyboards,notes_bot/frontends/telegram/tgstates
GO_COVERPKGS_UNIT = notes_bot/core,notes_bot/core/features,notes_bot/notifications,$(TELEGRAM_COVERPKGS)
GO_COVERPKGS_ALL  = notes_bot/core,notes_bot/core/features,notes_bot/notifications,$(TELEGRAM_COVERPKGS)

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
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -func=combined.out
	@rm -f unit.out integration.out combined.out

cover-html:
	go test -coverprofile=unit.out -coverpkg=$(GO_COVERPKGS_ALL) $(GO_UNIT_PKGS)
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
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

proto:
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notes.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notifications.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/whisper.proto

.PHONY: install test-go test-go-cover test-go-cover-html cover cover-html test-integration test-notifications test clean build-core build-notifications build-telegram up up-ci deploy down logs restart docker-clean proto format
