DOCKER_COMPOSE = docker compose

install:
	poetry install

run:
	poetry run python main.py

test:
	poetry run pytest -v

format:
	poetry run ruff check --fix --unsafe-fixes core frontends/telegram tests
	poetry run ruff format core frontends/telegram tests

clean:
	find . -type f -name '*.pyc' -delete
	find . -type d -name '__pycache__' -exec rm -rf {} +

up:
	$(DOCKER_COMPOSE) down
	$(DOCKER_COMPOSE) build --no-cache
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
	$(DOCKER_COMPOSE) up -d
	$(DOCKER_COMPOSE) logs -f

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

.PHONY: install run test clean up deploy down restart docker-clean proto
