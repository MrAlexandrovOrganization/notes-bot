.PHONY: migrate-automations test-automations-db
migrate-automations:
	$(DOCKER_COMPOSE) up -d postgres
	$(DOCKER_COMPOSE) build notifications
	$(DOCKER_COMPOSE) run --rm --no-deps notifications /notifications migrate-automations
test-automations-db:
	@test -n "$(TEST_AUTOMATIONS_DATABASE_URL)" || { echo 'Set TEST_AUTOMATIONS_DATABASE_URL to a disposable loopback PostgreSQL'; exit 1; }
	go test -race -count=1 ./internal/automations
