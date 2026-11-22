"""Restricted SSH receiver: bounded stream, immutable APK, atomic manifest, no shell."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import signal
import tempfile

MAX_APK = 64 * 1024 * 1024


def receive(stream, directory):
    header = stream.readline(8193)
    if len(header) > 8192 or not header.endswith(b"\n"):
        raise ValueError("invalid release header")
    manifest = json.loads(header)
    if set(manifest) != {"package_id", "version_code", "version_name", "sha256", "bytes", "certificate_sha256", "revision", "min_sdk"}:
        raise ValueError("invalid manifest fields")
    if (manifest["package_id"] != "ru.mralexandrov.notes"
        or type(manifest["version_code"]) is not int or not 0 < manifest["version_code"] <= 2100000000
        or type(manifest["bytes"]) is not int or not 0 < manifest["bytes"] <= MAX_APK
        or type(manifest["min_sdk"]) is not int or not 26 <= manifest["min_sdk"] <= 100
        or not isinstance(manifest["version_name"], str) or not 0 < len(manifest["version_name"]) <= 100
        or not re.fullmatch("[0-9a-f]{64}", manifest["sha256"])
        or not re.fullmatch("[0-9a-f]{64}", manifest["certificate_sha256"])
        or not re.fullmatch("[0-9a-f]{40}", manifest["revision"])):
        raise ValueError("invalid release metadata")
    directory = Path(directory)
    directory.mkdir(mode=0o755, parents=True, exist_ok=True)
    with (directory / ".publish.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        current = directory / "current.json"
        if current.exists():
            previous = json.loads(current.read_text())
            if previous["version_code"] > manifest["version_code"]:
                raise ValueError("refusing an older release")
            if previous["version_code"] == manifest["version_code"] and previous != manifest:
                raise ValueError("version code already belongs to another release")
            if previous["certificate_sha256"] != manifest["certificate_sha256"]:
                raise ValueError("refusing a different signing identity")
        tmp = None
        try:
            with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
                tmp = Path(output.name)
                digest = hashlib.sha256()
                remaining = manifest["bytes"]
                while remaining:
                    chunk = stream.read(min(65536, remaining))
                    if not chunk:
                        raise ValueError("incomplete APK")
                    output.write(chunk)
                    digest.update(chunk)
                    remaining -= len(chunk)
                if stream.read(1) or digest.hexdigest() != manifest["sha256"]:
                    raise ValueError("APK digest or length mismatch")
                output.flush()
                os.fsync(output.fileno())
            tmp.chmod(0o644)
            os.replace(tmp, directory / (manifest["sha256"] + ".apk"))
            tmp = None
            if current.exists():
                (directory / "previous.json").write_bytes(current.read_bytes())
            with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
                tmp = Path(output.name)
                output.write(json.dumps(manifest, sort_keys=True).encode())
                output.flush()
                os.fsync(output.fileno())
            tmp.chmod(0o644)
            os.replace(tmp, current)
            tmp = None
            fd = os.open(directory, os.O_RDONLY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        finally:
            if tmp is not None:
                tmp.unlink(missing_ok=True)
    return manifest


if __name__ == "__main__":
    signal.alarm(180)
    parser = argparse.ArgumentParser()
    parser.add_argument("--directory", required=True)
    args = parser.parse_args()
    if os.environ.get("SSH_ORIGINAL_COMMAND") != "publish-mobile-release":
        sys.exit("Only mobile release publication is permitted")
    try:
        result = receive(sys.stdin.buffer, args.directory)
    except (ValueError, OSError, KeyError, TypeError):
        sys.exit("Release rejected; existing release preserved")
    print("Published mobile version", result["version_code"], result["sha256"])
