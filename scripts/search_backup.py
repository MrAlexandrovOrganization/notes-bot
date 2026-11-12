"""Independent search-index backups; an offline WSL never blocks core backups."""

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import uuid

import notes_backup as common

NAME = re.compile(r"notes-search-(\d{8}T\d{6}Z)-[0-9a-f]{32}\.tar")
UTC = dt.timezone.utc


def digest(stream):
    hasher = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        hasher.update(chunk)
    return hasher.hexdigest()


def checksum(path):
    with Path(path).open("rb") as stream:
        return digest(stream)


def source():
    if os.environ.get("SSH_ORIGINAL_COMMAND", "notes-search-backup") != "notes-search-backup":
        raise ValueError("Unsupported backup command")
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="notes-search-export-") as temporary:
        root = Path(temporary)
        with common.deployment_lock(Path.home()):
            container, image = common.database("postgres-search", project="notes-search")
            common.check_tables(container, "search")
            with (root / "search.dump").open("wb") as output:
                common.run(["docker", "exec", container, "sh", "-c",
                            'exec timeout -k 10s 12m pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -Z1'], output=output)
            with (root / "search.dump").open("rb") as stream:
                common.run(["docker", "exec", "-i", container, "pg_restore", "--list"], source=stream)
            metadata = json.loads(common.run(["docker", "image", "inspect", image]))[0]
            manifest = {"format": 1, "kind": "notes-search", "created_at": dt.datetime.now(UTC).isoformat(),
                        "database": {"image": image, "fingerprint": common.image_fingerprint(metadata)},
                        "sha256": checksum(root / "search.dump")}
            (root / "manifest.json").write_text(json.dumps(manifest) + "\n")
        with tarfile.open(fileobj=sys.stdout.buffer, mode="w|") as archive:
            for name in ("manifest.json", "search.dump"):
                info = archive.gettarinfo(str(root / name), arcname=name)
                info.mode = 0o600
                with (root / name).open("rb") as stream:
                    archive.addfile(info, stream)


def validate(path):
    with tarfile.open(path, "r:") as archive:
        members = archive.getmembers()
        if len(members) != 2 or {m.name for m in members} != {"manifest.json", "search.dump"}:
            raise ValueError("Unexpected search archive members")
        if any(not m.isfile() for m in members) or archive.getmember("manifest.json").size > 65536:
            raise ValueError("Unsafe search archive members")
        manifest = json.load(archive.extractfile("manifest.json"))
        if manifest.get("format") != 1 or manifest.get("kind") != "notes-search":
            raise ValueError("Unsupported search backup format")
        if not re.fullmatch(r"[A-Za-z0-9./:_-]+@sha256:[0-9a-f]{64}", manifest["database"]["image"]):
            raise ValueError("Search backup lacks an immutable image")
        with archive.extractfile("search.dump") as stream:
            if digest(stream) != manifest["sha256"]:
                raise ValueError("Search backup checksum mismatch")
        return manifest


def pull(config):
    os.umask(0o077)
    root = Path(config["directory"])
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", config["host"]) or not re.fullmatch(r"[A-Za-z0-9_-]+", config["user"]):
        raise ValueError("Invalid backup source")
    with common.locked(root):
        name = "notes-search-" + dt.datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex + ".tar"
        partial = root / (name + ".partial")
        try:
            command = ["ssh", "-i", config["identity"], "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
                       "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + config["known_hosts"],
                       "-o", "HostKeyAlias=notes-search-backup", "-o", "ConnectTimeout=10",
                       "-o", "KexAlgorithms=curve25519-sha256", "-o", "ServerAliveInterval=15",
                       "-o", "ServerAliveCountMax=3", config["user"] + "@" + config["host"], "notes-search-backup"]
            with partial.open("wb") as output:
                common.run(command, output=output, timeout=900)
            manifest = validate(partial)
            target = root / name
            partial.rename(target)
            status = {"file": name, "sha256": checksum(target), "created_at": manifest["created_at"]}
            common.write_status(root, "latest.json", status)
            # Keep every hourly copy for 48 hours and one per month thereafter.
            paths = sorted((p for p in root.glob("notes-search-*.tar") if NAME.fullmatch(p.name)), reverse=True)
            months = set()
            now = dt.datetime.now(UTC)
            for path in paths:
                when = dt.datetime.strptime(NAME.fullmatch(path.name)[1], "%Y%m%dT%H%M%SZ").replace(tzinfo=UTC)
                month = (when.year, when.month)
                keep = now - when <= dt.timedelta(days=2) or (month not in months and now - when <= dt.timedelta(days=370))
                months.add(month)
                if not keep:
                    path.unlink()
            print(json.dumps({"event": "search backup completed", **status}))
        finally:
            partial.unlink(missing_ok=True)


def verify(config):
    root = Path(config["directory"])
    with tempfile.TemporaryDirectory(prefix="notes-search-restore-") as temporary:
        temporary = Path(temporary)
        with common.locked(root):
            latest = json.loads((root / "latest.json").read_text())
            if not NAME.fullmatch(latest["file"]):
                raise ValueError("Invalid latest search backup")
            path = root / latest["file"]
            if checksum(path) != latest["sha256"]:
                raise ValueError("Search backup changed")
            manifest = validate(path)
            with tarfile.open(path, "r:") as archive, archive.extractfile("search.dump") as source_stream, (temporary / "search.dump").open("wb") as output:
                shutil.copyfileobj(source_stream, output)
        image = common.restore_image(manifest["database"], config)
        name = "notes-search-restore-" + uuid.uuid4().hex[:12]
        try:
            common.run(["docker", "run", "-d", "--name", name, "--network=none", "--memory=512m", "--cpus=1",
                        "--security-opt=no-new-privileges:true", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
                        "-e", "POSTGRES_DB=restore", image])
            for _ in range(90):
                ready = subprocess.run(["docker", "exec", name, "pg_isready", "-U", "postgres", "-d", "restore"], capture_output=True, timeout=10)
                if ready.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise ValueError("Isolated search restore did not become ready")
            with (temporary / "search.dump").open("rb") as stream:
                common.run(["docker", "exec", "-i", name, "pg_restore", "-U", "postgres", "-d", "restore",
                            "--no-owner", "--no-privileges", "--single-transaction", "--exit-on-error"], source=stream)
            common.check_tables(name, "search")
            status = {"file": latest["file"], "verified_at": dt.datetime.now(UTC).isoformat()}
            common.write_status(root, "verified.json", status)
            print(json.dumps({"event": "isolated search restore verified", **status}))
        finally:
            subprocess.run(["docker", "rm", "-fv", name], capture_output=True, timeout=60)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("source", "pull", "verify"))
    parser.add_argument("--config", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "source":
            source()
        else:
            {"pull": pull, "verify": verify}[args.command](json.loads(args.config.read_text()))
    except Exception:
        raise SystemExit("Search backup failed; previous copies retained; details withheld") from None


if __name__ == "__main__":
    main()
