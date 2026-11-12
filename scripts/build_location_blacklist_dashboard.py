#!/usr/bin/env python3
"""Build Location dashboards from their templates and SQL sources."""

import argparse
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DASHBOARDS = ("location-blacklist", "location-zones")


def build(name):
    dashboard = json.loads((ROOT / f"monitoring/templates/{name}.json").read_text(encoding="utf-8"))
    sql_dir = ROOT / f"monitoring/sql/{name}"
    def sql(filename):
        if Path(filename).name != filename or not filename.endswith(".sql"):
            raise ValueError("SQL source must name a file in the dashboard SQL directory")
        return (sql_dir / filename).read_text(encoding="utf-8").strip()

    for variable in dashboard.get("templating", {}).get("list", []):
        filename = variable.pop("queryFile", None)
        if filename is not None:
            variable["query"] = sql(filename)
            variable["definition"] = variable["query"]
    for panel in dashboard["panels"]:
        for target in panel.get("targets", []):
            filename = target.pop("rawSqlFile", None)
            if filename is not None:
                target["rawSql"] = sql(filename)
    return json.dumps(dashboard, ensure_ascii=False, indent=2) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check without rewriting the dashboard")
    args = parser.parse_args()
    for name in DASHBOARDS:
        output = ROOT / f"monitoring/grafana-dashboards/{name}.json"
        rendered = build(name)
        if args.check:
            if not output.exists() or output.read_text(encoding="utf-8") != rendered:
                parser.exit(1, f"{name} dashboard is stale; run make monitoring-dashboard\n")
            print(f"{name} dashboard is up to date")
        else:
            output.write_text(rendered, encoding="utf-8")
            print(f"Built {output.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
