"""Publish a one-way read-only vault replica; never connect a second sync client."""

import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import time
import uuid


@contextlib.contextmanager
def lock(path, nonblocking=False):
    fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "rb") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX | (fcntl.LOCK_NB if nonblocking else 0))
        yield


def transfer(config, destination, previous):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", config["user"]):
        raise ValueError("Invalid source user")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", config["host"]):
        raise ValueError("Invalid source host")
    ssh = ["ssh", "-i", config["identity"], "-o", "IdentitiesOnly=yes",
           "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
           "-o", "UserKnownHostsFile=" + config["known_hosts"],
           "-o", "HostKeyAlias=notes-vault-source", "-o", "ConnectTimeout=5",
           "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2",
           "-o", "KexAlgorithms=curve25519-sha256"]
    # Only Markdown and directories are needed. Preserve sub-second mtimes;
    # never use --inplace with hard-linked immutable generations.
    command = ["rsync", "-rtlc", "--safe-links", "--delay-updates",
               "--chmod=Du=rwx,Dgo=,Fu=rw,Fgo=", "--exclude=.*/"]
    for directory in config.get("ignore_dirs", []):
        if not re.fullmatch(r"[\w .-]+", directory) or directory in {".", ".."}:
            raise ValueError("Invalid ignored directory")
        command += ["--exclude=" + directory + "/"]
    command += ["--include=*/", "--include=*.[mM][dD]", "--exclude=*"]
    if previous is not None:
        command += ["--link-dest=" + str(previous)]
    command += ["-e", shlex.join(ssh), config["user"] + "@" + config["host"] + ":./", str(destination) + "/"]
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=240)


def fingerprint(directory):
    rows = []
    for path in sorted(directory.rglob("*")):
        if path.is_symlink():
            resolved = path.resolve()
            if not resolved.is_relative_to(directory):
                raise ValueError("Snapshot contains an external symlink")
        if not path.is_file() or path.suffix.lower() != ".md":
            continue
        with path.open("rb") as stream:
            hasher = hashlib.sha256()
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                hasher.update(chunk)
            digest = hasher.hexdigest()
        info = path.stat()
        rows.append([path.relative_to(directory).as_posix(), info.st_size, info.st_mtime_ns, digest])
    if not rows:
        raise ValueError("Refusing to publish an empty vault snapshot")
    return len(rows), hashlib.sha256(json.dumps(rows, ensure_ascii=True).encode()).hexdigest()


def publish(config, copy=transfer):
    os.umask(0o077)
    root = Path(config["directory"]).resolve()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    generations = root / "generations"
    generations.mkdir(exist_ok=True, mode=0o700)
    with lock(root / ".sync.lock", nonblocking=True):
        # Create this once. Search holds a shared lock on this exact inode.
        with lock(root / ".snapshot.lock"):
            pass
        previous = (root / "current").resolve() if (root / "current").is_symlink() else None
        if previous is not None and previous.parent != generations:
            raise ValueError("Current snapshot is outside the generation directory")
        name = uuid.uuid4().hex
        stage = generations / (".staging-" + name)
        stage.mkdir(mode=0o700)
        try:
            copy(config, stage, previous)
            count, digest = fingerprint(stage)
            state_path = root / "state.json"
            old = json.loads(state_path.read_text()) if state_path.exists() else {}
            with lock(root / ".snapshot.lock"):
                if old.get("fingerprint") == digest and previous is not None:
                    generation = previous.name
                else:
                    final = generations / name
                    stage.rename(final)
                    temporary_link = root / (".current-" + name)
                    temporary_link.symlink_to("generations/" + name)
                    os.replace(temporary_link, root / "current")
                    generation = name
                keep = {generation, previous.name if previous else ""}
                for path in generations.iterdir():
                    if re.fullmatch(r"[0-9a-f]{32}", path.name) and path.name not in keep and not path.is_symlink():
                        shutil.rmtree(path)
                state = {"generation": generation, "files": count, "fingerprint": digest,
                         "last_success": time.time()}
                temporary_state = root / ".state.json.new"
                temporary_state.write_text(json.dumps(state) + "\n")
                os.replace(temporary_state, state_path)
            return state
        finally:
            if stage.exists():
                shutil.rmtree(stage)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    args = parser.parse_args()
    try:
        publish(json.loads(args.config.read_text()))
    except BlockingIOError:
        return  # An earlier sync or deployment owns the same snapshot.
    except Exception:
        # rsync/SSH stderr may include filenames; keep diagnostics private.
        raise SystemExit("Search vault snapshot failed; previous generation retained") from None


if __name__ == "__main__":
    main()
