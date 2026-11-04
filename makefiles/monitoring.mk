# Python 3 (standard library only) assembles SQL into the Grafana resource.
.PHONY: monitoring-dashboard monitoring-check
monitoring-dashboard:
	python3 -B scripts/build_location_blacklist_dashboard.py

monitoring-check:
	python3 -B scripts/build_location_blacklist_dashboard.py --check

format: monitoring-dashboard
check: monitoring-check
monitoring-register: monitoring-check
