install:
	poetry install

run:
	poetry run python main.py

format:
	poetry run ruff check --fix --unsafe-fixes core frontends/telegram
	poetry run ruff format core frontends/telegram

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

.PHONY: install run clean up down restart docker-clean
