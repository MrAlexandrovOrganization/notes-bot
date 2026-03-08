DOCKER_COMPOSE = docker compose

install:
	poetry install

# All Go unit test packages (no integration)
GO_UNIT_PKGS = ./core/... ./core/features/... ./notifications/...

# All packages to instrument for coverage
GO_COVERPKGS_UNIT = notes_bot/core,notes_bot/core/features,notes_bot/notifications
GO_COVERPKGS_ALL  = notes_bot/core,notes_bot/core/features,notes_bot/notifications

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

cover-all:
	go test -coverprofile=unit.out -coverpkg=$(GO_COVERPKGS_ALL) $(GO_UNIT_PKGS)
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -func=combined.out
	@rm -f unit.out integration.out combined.out

cover-all-html:
	go test -coverprofile=unit.out -coverpkg=$(GO_COVERPKGS_ALL) $(GO_UNIT_PKGS)
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -html=combined.out
	@rm -f unit.out integration.out combined.out

test-integration:
	go test ./integration/... -v

test-notifications:
	go test ./notifications/... -v

test-all: test-go test-integration

build-notifications:
	$(DOCKER_COMPOSE) build notifications

build-telegram:
	$(DOCKER_COMPOSE) build telegram

format:
	poetry run ruff check --fix --unsafe-fixes whisper
	poetry run ruff format whisper
	gofmt -w ./notifications/ ./frontends/telegram/ ./cmd/

clean:
	find . -type f -name '*.pyc' -delete
	find . -type d -name '__pycache__' -exec rm -rf {} +

up:
	$(DOCKER_COMPOSE) down
	$(DOCKER_COMPOSE) build
	$(DOCKER_COMPOSE) up -d
	$(DOCKER_COMPOSE) logs -f

deploy:
	$(DOCKER_COMPOSE) down
	$(DOCKER_COMPOSE) build --no-cache
	$(DOCKER_COMPOSE) up -d

down:
	$(DOCKER_COMPOSE) down

logs:
	$(DOCKER_COMPOSE) logs -f

restart:
	$(DOCKER_COMPOSE) down
	$(DOCKER_COMPOSE) build --no-cache
	$(DOCKER_COMPOSE) up -d
	$(DOCKER_COMPOSE) logs -f

build-core:
	$(DOCKER_COMPOSE) build core

docker-clean:
	$(DOCKER_COMPOSE) down --rmi all --volumes --remove-orphans
	docker system prune -f

proto:
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto --mypy_out=proto --mypy_grpc_out=proto proto/whisper.proto
	sed -i.bak 's/^import whisper_pb2/from proto import whisper_pb2/' proto/whisper_pb2_grpc.py && rm -f proto/whisper_pb2_grpc.py.bak

	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notes.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notifications.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/whisper.proto

.PHONY: install test-go test-go-cover test-go-cover-html cover-all cover-all-html test-integration test-notifications test-all clean build-core build-notifications build-telegram up deploy down logs restart docker-clean proto format
