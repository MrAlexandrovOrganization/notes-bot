.PHONY: migrate-geo
migrate-geo:
	$(DOCKER_COMPOSE) up -d postgres
	$(DOCKER_COMPOSE) build notifications
	$(DOCKER_COMPOSE) run --rm --no-deps notifications /notifications migrate-geo

.PHONY: test-geo-db
test-geo-db:
	@test -n "$(TEST_GEO_DATABASE_URL)" || { echo 'Set TEST_GEO_DATABASE_URL to an isolated loopback PostgreSQL'; exit 1; }
	go test -race -count=1 ./internal/georeminders

.PHONY: proto-location-sync proto-location-check
proto-location-sync:
	@test -n "$(LOCATION_PROTO_SRC)" -a -n "$(LOCATION_SOURCE_REVISION)" || { echo 'Set LOCATION_PROTO_SRC and LOCATION_SOURCE_REVISION'; exit 1; }
	python3 scripts/location-contract.py --source '$(LOCATION_PROTO_SRC)' --revision '$(LOCATION_SOURCE_REVISION)'
proto-location-check:
	python3 scripts/location-contract.py
	buf format --diff --exit-code proto --path proto/location --path proto/georeminders
	buf build proto
check: proto-location-check
