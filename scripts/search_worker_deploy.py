"""Deploy through a pinned SSH jump without putting registry credentials in argv."""

import json
import os
import re
import subprocess
import tempfile
from pathlib import Path


def endpoint(value):
    if set(value) != {"host", "port", "user"}:
        raise ValueError("Invalid SSH endpoint")
    if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,253}", value["host"]):
        raise ValueError("Invalid SSH host")
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_-]{0,63}", value["user"]):
        raise ValueError("Invalid SSH user")
    if type(value["port"]) is not int or not 1 <= value["port"] <= 65535:
        raise ValueError("Invalid SSH port")
    return value


def main():
    transport = json.loads(os.environ["SEARCH_WORKER_TRANSPORT_JSON"])
    if set(transport) != {"jump", "target"}:
        raise ValueError("Invalid deployment transport")
    payload = {
        key: os.environ[key]
        for key in (
            "DEPLOY_SHA",
            "IMAGE_MANIFEST_B64",
            "IMAGE_REPOSITORY",
            "GHCR_USER",
            "GHCR_TOKEN",
        )
    }
    with tempfile.TemporaryDirectory(prefix="notes-search-deploy-") as tmp:
        root = Path(tmp)
        for name, variable in (
            ("key", "SEARCH_WORKER_SSH_KEY"),
            ("known_hosts", "SEARCH_WORKER_KNOWN_HOSTS"),
        ):
            path = root / name
            path.write_text(os.environ[variable].rstrip() + "\n")
            path.chmod(0o600)
        config = [
            "Host *",
            "  BatchMode yes",
            "  IdentitiesOnly yes",
            "  StrictHostKeyChecking yes",
            f"  IdentityFile {root / 'key'}",
            f"  UserKnownHostsFile {root / 'known_hosts'}",
            "  ConnectTimeout 10",
            # The scoped tailnet jump stalls on the larger hybrid KEX packets.
            "  KexAlgorithms curve25519-sha256",
            "  ServerAliveInterval 15",
            "  ServerAliveCountMax 4",
            "  LogLevel ERROR",
        ]
        for name in ("jump", "target"):
            item = endpoint(transport[name])
            alias = "notes-search-" + name
            config += [
                "Host " + alias,
                "  HostName " + item["host"],
                "  Port " + str(item["port"]),
                "  User " + item["user"],
                "  HostKeyAlias " + alias,
            ]
            if name == "target":
                config.append("  ProxyJump notes-search-jump")
        path = root / "ssh_config"
        path.write_text("\n".join(config) + "\n")
        path.chmod(0o600)
        subprocess.run(
            ["ssh", "-F", str(path), "notes-search-target", "notes-search-deploy"],
            input=json.dumps(payload).encode(),
            check=True,
            timeout=1800,
        )


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit(
            "Search worker SSH deployment failed; credentials suppressed"
        ) from None
