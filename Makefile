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
	docker-compose down
	docker-compose build --no-cache
	docker-compose up -d
	docker-compose logs -f

down:
	docker-compose down

logs:
	docker-compose logs -f

restart:
	docker-compose down
	docker-compose up -d
	docker-compose logs -f

docker-clean:
	docker-compose down --rmi all --volumes --remove-orphans
	docker system prune -f

proto:
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto proto/notes.proto
	poetry run python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto proto/notifications.proto
	sed -i '' 's/^import notes_pb2/from proto import notes_pb2/' proto/notes_pb2_grpc.py
	sed -i '' 's/^import notifications_pb2/from proto import notifications_pb2/' proto/notifications_pb2_grpc.py

.PHONY: install run test clean up down restart docker-clean proto
