#!/usr/bin/env python3
"""Install dashboards using the folder destinations in the observability manifest."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import tempfile


ROOT = Path(__file__).resolve().parents[1]


def destination(directory, relative):
    path = PurePosixPath(relative)
    if path.is_absolute() or any(p in {"", ".", ".."} for p in relative.split("/")):
        raise ValueError("Invalid dashboard destination")
    target = directory.joinpath(*path.parts)
    if directory.is_symlink():
        raise ValueError("Dashboard directory must not be a symlink")
    if target.is_symlink() or any(directory.joinpath(*path.parts[:i]).is_symlink() for i in range(1, len(path.parts))):
        raise ValueError("Dashboard paths must not be symlinks")
    return target


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--unregister", action="store_true")
    args = parser.parse_args()
    directory = args.directory.absolute()
    doc = json.loads((ROOT / "monitoring/observability.json").read_text())
    if args.unregister:
        owned = []
        for asset in doc["assets"]:
            if asset["kind"] != "dashboards":
                continue
            target = destination(directory, asset["destination"])
            if target.exists():
                if target.read_bytes() != (ROOT / asset["file"]).read_bytes():
                    raise ValueError(f"Existing dashboard was modified: {asset['destination']}")
                owned.append(target)
        for target in owned:
            target.unlink()
        print("Owned dashboard files unregistered")
        return
    retire = []
    # Validate all removals before writing, to avoid a partial folder migration.
    for asset in doc.get("retire", []):
        if asset["kind"] != "dashboards":
            continue
        old = destination(directory, asset["destination"])
        if old.exists():
            if hashlib.sha256(old.read_bytes()).hexdigest() not in asset["sha256"]:
                raise ValueError(f"Existing dashboard was modified: {asset['destination']}")
            retire.append(old)
    for asset in doc["assets"]:
        if asset["kind"] != "dashboards":
            continue
        target = destination(directory, asset["destination"])
        body = (ROOT / asset["file"]).read_bytes()
        target.parent.mkdir(parents=True, exist_ok=True)
        fd, temporary = tempfile.mkstemp(prefix=".dashboard-", dir=target.parent)
        try:
            with os.fdopen(fd, "wb") as stream:
                stream.write(body)
            os.chmod(temporary, 0o644)
            os.replace(temporary, target)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    for old in retire:
        old.unlink()
    print("Dashboard folders registered")


if __name__ == "__main__":
    main()
