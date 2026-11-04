#!/usr/bin/env python3
"""Build the Location blacklist dashboard from its template and SQL sources."""

import argparse
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
TEMPLATE = ROOT / "monitoring/templates/location-blacklist.json"
SQL_DIR = ROOT / "monitoring/sql/location-blacklist"
OUTPUT = ROOT / "monitoring/grafana-dashboards/location-blacklist.json"


def build():
    dashboard = json.loads(TEMPLATE.read_text(encoding="utf-8"))
    for panel in dashboard["panels"]:
        for target in panel.get("targets", []):
            filename = target.pop("rawSqlFile", None)
            if filename is not None:
                if Path(filename).name != filename or not filename.endswith(".sql"):
                    raise ValueError("rawSqlFile must name a SQL file in the dashboard SQL directory")
                target["rawSql"] = (SQL_DIR / filename).read_text(encoding="utf-8").strip()
    return json.dumps(dashboard, ensure_ascii=False, indent=2) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check without rewriting the dashboard")
    args = parser.parse_args()
    rendered = build()
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text(encoding="utf-8") != rendered:
            parser.exit(1, "Location blacklist dashboard is stale; run make monitoring-dashboard\n")
        print("Location blacklist dashboard is up to date")
    else:
        OUTPUT.write_text(rendered, encoding="utf-8")
        print("Built monitoring/grafana-dashboards/location-blacklist.json")


if __name__ == "__main__":
    main()
