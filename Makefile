DOCKER_COMPOSE = docker compose

install:
	poetry install

run:
	poetry run python main.py

test:
	poetry run pytest -v

test-go:
	go test ./core/... ./core/features/...

test-go-cover:
	go test -coverprofile=coverage.out ./core/... ./core/features/...
	go tool cover -func=coverage.out
	@rm -f coverage.out

test-go-cover-html:
	go test -coverprofile=coverage.out ./core/... ./core/features/...
	go tool cover -html=coverage.out
	@rm -f coverage.out

cover-all:
	go test -coverprofile=unit.out ./core/... ./core/features/...
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -func=combined.out
	@rm -f unit.out integration.out combined.out

cover-all-html:
	go test -coverprofile=unit.out ./core/... ./core/features/...
	go test -coverprofile=integration.out -coverpkg=notes_bot/core,notes_bot/core/features ./integration/...
	@{ cat unit.out; tail -n +2 integration.out; } > combined.out
	go tool cover -html=combined.out
	@rm -f unit.out integration.out combined.out

test-integration:
	go test ./integration/... -v

test-all: test test-go test-integration

format:
	poetry run ruff check --fix --unsafe-fixes frontends/telegram tests notifications whisper
	poetry run ruff format frontends/telegram tests notifications whisper

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
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto --mypy_out=proto --mypy_grpc_out=proto proto/notes.proto
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto --mypy_out=proto --mypy_grpc_out=proto proto/notifications.proto
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto --mypy_out=proto --mypy_grpc_out=proto proto/whisper.proto
	sed -i.bak 's/^import notes_pb2/from proto import notes_pb2/' proto/notes_pb2_grpc.py && rm -f proto/notes_pb2_grpc.py.bak
	sed -i.bak 's/^import notifications_pb2/from proto import notifications_pb2/' proto/notifications_pb2_grpc.py && rm -f proto/notifications_pb2_grpc.py.bak
	sed -i.bak 's/^import whisper_pb2/from proto import whisper_pb2/' proto/whisper_pb2_grpc.py && rm -f proto/whisper_pb2_grpc.py.bak

	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notes.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/notifications.proto
	protoc -I=proto --go_out=. --go_opt=module=notes_bot --go-grpc_out=. --go-grpc_opt=module=notes_bot proto/whisper.proto

.PHONY: install run test test-go test-go-cover test-go-cover-html cover-all cover-all-html test-integration test-all clean build-core up deploy down logs restart docker-clean proto
