#!/usr/bin/env python3
"""Build Grafana dashboards from their templates and SQL sources."""

import argparse
import copy
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DASHBOARDS = ("location", "location-zones", "location-context", "notes-english", "notes-journal")


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
        route_config = panel.pop("routeGeneration", None)
        if route_config is not None:
            layers, targets = [], []
            variants = (
                ("JKLM", " · со всеми точками", 0.25, "TRUE"),
                ("EFGH", " · со скрытыми вне блэклиста", 0.5,
                 "(NOT hidden OR NOT public.location_blacklist_matches(latitude, longitude))"),
                ("ABCD", "", 1, "NOT hidden"),
            )
            for refs, suffix, opacity, condition in variants:
                for profile, ref in zip(route_config["profiles"], refs):
                    source = profile["source"]
                    if source not in {"android", "ios", "telegram-bot", "unknown"}:
                        raise ValueError("Unknown route source")
                    layer = copy.deepcopy(route_config["routeLayer"])
                    layer["name"] = profile["name"] + suffix
                    layer["filterData"]["options"] = ref
                    layer["config"]["style"]["color"]["fixed"] = profile["color"]
                    layer["config"]["style"]["opacity"] = opacity
                    layers.append(layer)
                    target = copy.deepcopy(route_config["routeTarget"])
                    target["refId"] = ref
                    target.pop("rawSqlFile", None)
                    target["rawSql"] = sql("route-source.sql").replace("__SOURCE__", source).replace("__CONDITION__", condition)
                    targets.append(target)
            panel["options"]["layers"] = layers + panel["options"]["layers"]
            panel["targets"] = targets + panel["targets"]
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
