MOBILE_COMPOSE = $(DOCKER_COMPOSE_BASE) $(COMPOSE_FLAGS) -f compose/compose.mobile.yml
.PHONY: mobile-config-check mobile-up mobile-status mobile-stop
mobile-config-check:
	$(MOBILE_COMPOSE) config --quiet
mobile-up: assert-active mobile-config-check
	$(MOBILE_COMPOSE) pull mobile
	$(MOBILE_COMPOSE) up -d --no-deps --no-build mobile
mobile-status:
	$(MOBILE_COMPOSE) ps mobile
mobile-stop: assert-active
	$(MOBILE_COMPOSE) stop mobile
