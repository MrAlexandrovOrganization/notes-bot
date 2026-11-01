"""Private notes vault and PostgreSQL backups; Python 3.12+, OpenSSH and Docker."""

import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import uuid

NAME = re.compile(r"notes-bot-(\d{8}T\d{6}Z)-[0-9a-f]{32}\.tar")
MEMBERS = {"vault.tgz", "yandex-config.tgz", "application.env", "notifications.dump", "search.dump", "manifest.json"}
REQUIRED_TABLES = {
    "notifications": {"reminders", "voice_jobs", "voice_schema_migrations", "location_history", "location_tracking"},
    "search": {"notes", "note_chunks", "chunk_embeddings", "note_profiles", "profile_embeddings"},
}


class BackupError(RuntimeError):
    """A diagnostic that contains no credentials or note contents."""


def log(message, **fields):
    print(json.dumps({"time": dt.datetime.now(dt.UTC).isoformat(), "level": "info",
                      "service": "notes-backup", "message": message, **fields}), flush=True)


def run(command, *, output=None, source=None, timeout=900):
    result = subprocess.run(command, stdin=source, stdout=output or subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout, check=False)
    if result.returncode:
        raise BackupError(f"{Path(command[0]).name} failed (exit {result.returncode}); output withheld")
    return result.stdout


def checksum(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def image_fingerprint(metadata):
    value = [metadata.get("Architecture"), metadata.get("Os"), metadata.get("Config"),
             metadata["RootFS"]["Layers"]]
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def restore_image(database, config):
    cached = config.get("image_cache", {}).get(database["image"], {})
    expected = database.get("fingerprint", cached.get("fingerprint"))
    candidates = [database["image"]]
    if cached.get("reference"):
        candidates.append(cached["reference"])
    for reference in candidates:
        found = subprocess.run(["docker", "image", "inspect", reference], capture_output=True, timeout=30)
        if not found.returncode:
            if expected and image_fingerprint(json.loads(found.stdout)[0]) != expected:
                raise BackupError("Cached database image differs from source")
            if reference != database["image"] and not expected:
                raise BackupError("Imported restore image requires a source fingerprint")
            return reference
    run(["docker", "pull", "--quiet", database["image"]], timeout=600)
    return database["image"]


def inventory(root):
    """Includes dotfiles, untracked/ignored files and Git history; never follows symlinks."""
    result = {}
    for path in sorted(root.rglob("*")):
        name = path.relative_to(root).as_posix()
        if path.is_symlink():
            result[name] = {"type": "link", "target": os.readlink(path)}
        elif path.is_file():
            result[name] = {"type": "file", "size": path.stat().st_size, "sha256": checksum(path)}
        elif path.is_dir():
            result[name] = {"type": "directory"}
        else:
            raise BackupError("Unsupported vault filesystem entry")
    return result


def archive_inventory(path):
    result = {}
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            name = member.name
            if name in result or name.startswith("/") or ".." in Path(name).parts:
                raise BackupError("Unsafe or duplicate vault archive entry")
            if member.isfile():
                with archive.extractfile(member) as stream:
                    digest = hashlib.file_digest(stream, "sha256").hexdigest()
                result[name] = {"type": "file", "size": member.size, "sha256": digest}
            elif member.issym():
                result[name] = {"type": "link", "target": member.linkname}
            elif member.isdir():
                result[name] = {"type": "directory"}
            else:
                raise BackupError("Unsupported vault archive entry")
    return result


def fingerprint(entries):
    return hashlib.sha256(json.dumps(entries, sort_keys=True, ensure_ascii=True).encode()).hexdigest()


def snapshot_vault(root, destination):
    if not root.is_dir() or root.is_symlink():
        raise BackupError("Vault must be an existing real directory")
    for _ in range(3):
        try:
            before = inventory(root)
            with tarfile.open(destination, "w:gz", compresslevel=1, dereference=False) as archive:
                for name in before:
                    archive.add(root / name, arcname=name, recursive=False)
            if before == archive_inventory(destination) == inventory(root):
                return {"sha256": fingerprint(before), "entries": len(before),
                        "files": sum(v["type"] == "file" for v in before.values())}
        except FileNotFoundError:
            pass  # Concurrent rename/write: retry the whole snapshot, never publish a partial one.
    raise BackupError("Vault changed during all snapshot attempts")


def database(service):
    ids = run(["docker", "ps", "-q", "--filter", "label=com.docker.compose.project=notes-bot",
               "--filter", f"label=com.docker.compose.service={service}"]).decode().split()
    if len(ids) != 1:
        raise BackupError("Expected exactly one running database for each notes-bot service")
    container = json.loads(run(["docker", "inspect", ids[0]]))[0]
    image = json.loads(run(["docker", "image", "inspect", container["Image"]]))[0]
    digests = image.get("RepoDigests") or []
    if not digests:
        raise BackupError("Database image has no immutable repository digest")
    return ids[0], digests[0]


def check_tables(container, name):
    statement = "SELECT coalesce(json_agg(tablename), '[]') FROM pg_tables WHERE schemaname='public';"
    tables = json.loads(run(["docker", "exec", container, "sh", "-c",
                             'exec psql -XAtq -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-postgres}" -d "$POSTGRES_DB" -c "$1"',
                             "sh", statement]))
    if not REQUIRED_TABLES[name].issubset(tables):
        raise BackupError("Required notes database tables are missing")


@contextlib.contextmanager
def deployment_lock(home):
    """Do not let pg_dump overlap startup DDL under the existing CI host lock."""
    path = home / ".notes-bot-images-deploy.lock"
    owner = home.stat()
    created = False
    try:
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        created = True
    except FileExistsError:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as lock:
        if created:
            # The root exporter must not take ownership away from CI.
            os.fchown(lock.fileno(), owner.st_uid, owner.st_gid)
        metadata = os.fstat(lock.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != owner.st_uid:
            raise BackupError("Deployment lock must be a regular file owned by the application user")
        try:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            raise BackupError("Deployment in progress; backup will retry") from None
        yield


def source_export(root, home):
    os.umask(0o077)
    project = root / "telegram-bots/notes-bot"
    with tempfile.TemporaryDirectory(prefix="notes-export-") as temporary:
        directory = Path(temporary)
        with deployment_lock(home):
            vault = snapshot_vault(home / "Yandex.Disk/notes", directory / "vault.tgz")
            config = home / ".config/yandex-disk"
            if not (config / "config.cfg").is_file() or not (config / "passwd").is_file():
                raise BackupError("Yandex Disk configuration or auth file is missing")
            with tarfile.open(directory / "yandex-config.tgz", "w:gz", dereference=False) as archive:
                archive.add(config, arcname="yandex-disk")
            shutil.copyfile(project / ".env", directory / "application.env")
            databases = {}
            for service, name in (("postgres", "notifications"), ("postgres-search", "search")):
                container, image = database(service)
                check_tables(container, name)
                with (directory / f"{name}.dump").open("wb") as stream:
                    run(["docker", "exec", container, "sh", "-c",
                         'exec timeout -k 10s 12m pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -Z1'], output=stream)
                with (directory / f"{name}.dump").open("rb") as stream:
                    run(["docker", "exec", "-i", container, "pg_restore", "--list"], source=stream)
                metadata = json.loads(run(["docker", "image", "inspect", image]))[0]
                databases[name] = {"image": image, "fingerprint": image_fingerprint(metadata)}
            manifest = {"format": 1, "created_at": dt.datetime.now(dt.UTC).isoformat(),
                        "revision": run(["git", "-C", str(project), "rev-parse", "HEAD"]).decode().strip(),
                        "vault": vault, "databases": databases,
                        "sha256": {p.name: checksum(p) for p in directory.iterdir()}}
            (directory / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        with tarfile.open(fileobj=sys.stdout.buffer, mode="w|") as archive:
            for path in sorted(directory.iterdir()):
                info = archive.gettarinfo(str(path), arcname=path.name)
                info.mode = 0o600
                with path.open("rb") as stream:
                    archive.addfile(info, stream)


def validate(path):
    with tarfile.open(path, "r:") as archive:
        members = archive.getmembers()
        if len(members) != len(MEMBERS) or {m.name for m in members} != MEMBERS:
            raise BackupError("Unexpected backup archive members")
        if any(not m.isfile() for m in members) or archive.getmember("manifest.json").size > 65536:
            raise BackupError("Invalid backup archive members")
        manifest = json.load(archive.extractfile("manifest.json"))
        if manifest.get("format") != 1 or set(manifest["sha256"]) != MEMBERS - {"manifest.json"}:
            raise BackupError("Unsupported backup manifest")
        if set(manifest["databases"]) != {"notifications", "search"}:
            raise BackupError("Invalid database manifest")
        for db in manifest["databases"].values():
            if not re.fullmatch(r"[a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}", db["image"]):
                raise BackupError("Invalid database image digest")
        for name, expected in manifest["sha256"].items():
            with archive.extractfile(name) as stream:
                if hashlib.file_digest(stream, "sha256").hexdigest() != expected:
                    raise BackupError("Backup checksum mismatch")
        return manifest


def retained(paths, now):
    dated = []
    for path in paths:
        match = NAME.fullmatch(path.name)
        if match:
            dated.append((dt.datetime.strptime(match[1], "%Y%m%dT%H%M%SZ").replace(tzinfo=dt.UTC), path))
    dated.sort(reverse=True)
    keep = {p for when, p in dated if now - when <= dt.timedelta(days=2)}
    for key, limit in ((lambda t: t.date(), 7), (lambda t: t.isocalendar()[:2], 4),
                       (lambda t: (t.year, t.month), 12)):
        seen = set()
        for when, path in dated:
            bucket = key(when)
            if bucket not in seen and len(seen) < limit:
                keep.add(path)
                seen.add(bucket)
    return keep


def write_status(directory, name, value):
    path = directory / (".status-" + uuid.uuid4().hex)
    try:
        with path.open("x") as stream:
            json.dump(value, stream)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        path.replace(directory / name)
    finally:
        path.unlink(missing_ok=True)


@contextlib.contextmanager
def locked(directory):
    os.umask(0o077)
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    if directory.is_symlink() or directory.stat().st_mode & 0o077:
        raise BackupError("Backup directory must be private (0700) and not a symlink")
    with (directory / ".lock").open("a") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def pull(config):
    directory = Path(config["directory"])
    with locked(directory):
        space = os.statvfs(directory)
        if space.f_bavail * space.f_frsize < 5 * 1024**3:
            raise BackupError("Less than 5 GiB free at backup destination")
        destination = directory / ("notes-bot-" + dt.datetime.now(dt.UTC).strftime("%Y%m%dT%H%M%SZ")
                                   + "-" + uuid.uuid4().hex + ".tar")
        partial = destination.with_suffix(".partial")
        try:
            with partial.open("xb") as stream:
                run(["ssh", "-F", "/dev/null", "-T", "-i", config["identity"],
                     "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
                     "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=20",
                     "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
                     "-o", f"UserKnownHostsFile={config['known_hosts']}", config["source"], "export"],
                    output=stream, timeout=1800)
                stream.flush()
                os.fsync(stream.fileno())
            manifest = validate(partial)
            partial.replace(destination)
            status = {"time": dt.datetime.now(dt.UTC).isoformat(), "file": destination.name,
                      "bytes": destination.stat().st_size, "sha256": checksum(destination),
                      "vault": manifest["vault"]}
            write_status(directory, "latest.json", status)
            paths = [p for p in directory.iterdir() if NAME.fullmatch(p.name) and p.is_file()]
            keep = retained(paths, dt.datetime.now(dt.UTC))
            for path in paths:
                if path not in keep:
                    path.unlink()
            log("backup completed", **status, retained=len(keep))
        finally:
            partial.unlink(missing_ok=True)


def verify(config):
    directory = Path(config["directory"])
    with tempfile.TemporaryDirectory(prefix="notes-restore-") as temporary:
        temporary = Path(temporary)
        with locked(directory):
            latest = json.loads((directory / "latest.json").read_text())
            if not NAME.fullmatch(latest["file"]):
                raise BackupError("Invalid latest backup filename")
            path = directory / latest["file"]
            if checksum(path) != latest["sha256"]:
                raise BackupError("Latest backup checksum mismatch")
            manifest = validate(path)
            with tarfile.open(path, "r:") as archive:
                for name in ("vault.tgz", "notifications.dump", "search.dump"):
                    with archive.extractfile(name) as source, (temporary / name).open("wb") as output:
                        shutil.copyfileobj(source, output)
        if fingerprint(archive_inventory(temporary / "vault.tgz")) != manifest["vault"]["sha256"]:
            raise BackupError("Vault contents differ from source snapshot")
        restored = temporary / "vault"
        restored.mkdir()
        with tarfile.open(temporary / "vault.tgz", "r:gz") as archive:
            archive.extractall(restored, filter="data")
        if fingerprint(inventory(restored)) != manifest["vault"]["sha256"]:
            raise BackupError("Restored vault differs from source snapshot")
        if (restored / ".git").is_dir():
            run(["git", "-C", str(restored), "fsck", "--full"], timeout=300)
        counts = {}
        for name, database in manifest["databases"].items():
            image = restore_image(database, config)
            container = "notes-backup-verify-" + uuid.uuid4().hex[:12]
            created = False
            try:
                run(["docker", "run", "-d", "--pull=never", "--name", container,
                     "--network=none", "--label", "service=notes-backup-verify",
                     "--memory=512m", "--cpus=1", "--pids-limit=128",
                     "--security-opt=no-new-privileges:true",
                     "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "POSTGRES_DB=restore", image])
                created = True
                for _ in range(90):
                    ready = subprocess.run(["docker", "exec", container, "sh", "-c",
                                            'test "$(cat /proc/1/comm)" = postgres && pg_isready -U postgres -d restore'],
                                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
                    if ready.returncode == 0:
                        break
                    time.sleep(1)
                else:
                    raise BackupError("Restore fixture did not become ready")
                with (temporary / f"{name}.dump").open("rb") as stream:
                    run(["docker", "exec", "-i", container, "pg_restore", "-U", "postgres", "-d", "restore",
                         "--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction"], source=stream)
                check_tables(container, name)
                # Counts only; note/reminder/voice contents never enter logs.
                sql = """DO $$ DECLARE t record; BEGIN
                    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname='public' LOOP
                    EXECUTE format('SELECT count(*) FROM %I.%I', 'public', t.tablename);
                    END LOOP; END $$;
                    SELECT count(*) FROM pg_tables WHERE schemaname='public';"""
                counts[name] = run(["docker", "exec", container, "psql", "-XAtq", "-v", "ON_ERROR_STOP=1",
                                    "-U", "postgres", "-d", "restore", "-c", sql]).decode().strip()
            finally:
                if created:
                    run(["docker", "rm", "-fv", container], timeout=60)
        status = {"time": dt.datetime.now(dt.UTC).isoformat(), "file": path.name,
                  "vault": manifest["vault"], "database_tables": counts}
        write_status(directory, "verified.json", status)
        log("restore verified", **status)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("source", "pull", "verify"))
    parser.add_argument("--root", type=Path, default=Path("/home/maxim/projects"))
    parser.add_argument("--home", type=Path, default=Path("/home/maxim"))
    parser.add_argument("--config", type=Path, default=Path("/home/maxim/.config/notes-backup/config.json"))
    args = parser.parse_args()
    if args.command == "source":
        source_export(args.root, args.home)
    else:
        {"pull": pull, "verify": verify}[args.command](json.loads(args.config.read_text()))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"time": dt.datetime.now(dt.UTC).isoformat(), "level": "error",
                          "service": "notes-backup", "message": "backup operation failed",
                          "error_type": type(error).__name__,
                          "reason": str(error) if isinstance(error, BackupError) else "details withheld"}), file=sys.stderr)
        sys.exit(1)
