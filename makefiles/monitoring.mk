# Python 3 (standard library only) assembles SQL into the Grafana resource.
.PHONY: monitoring-dashboard monitoring-check english-reporting-check
monitoring-dashboard:
	python3 -B scripts/build_location_blacklist_dashboard.py

monitoring-check:
	python3 -B scripts/build_location_blacklist_dashboard.py --check

english-reporting-check:
	$(DOCKER) run --rm --network none -i haproxy:$(HAPROXY_VERSION) haproxy -c -f /dev/stdin < ops/english/reporting.cfg

format: monitoring-dashboard
check: monitoring-check english-reporting-check
monitoring-register: monitoring-check
