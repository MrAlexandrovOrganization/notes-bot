"""Refresh transitional tailnet forwards after shared Docker containers change IP."""

import ipaddress
import json
from pathlib import Path
import subprocess

TARGETS = (
    (9092, "kafka", "kafka", "kafka-net", 9092),
    (16379, "redis", "redis", "redis-net", 6379),
    (18084, "location", "location", "location-api-net", 8080),
)
STATE = Path("/var/lib/notes-legacy-forward/targets.json")


def run(args):
    result = subprocess.run(args, capture_output=True, timeout=30)
    if result.returncode:
        raise RuntimeError("Forwarder command failed; output withheld")
    return result.stdout


def main():
    old = json.loads(STATE.read_text()) if STATE.exists() else {}
    active = json.loads(run(["tailscale", "serve", "status", "--json"])).get("TCP", {})
    desired = {}
    for port, project, service, network, backend_port in TARGETS:
        ids = run(["docker", "ps", "-q", "--filter", f"label=com.docker.compose.project={project}",
                   "--filter", f"label=com.docker.compose.service={service}"]).decode().split()
        if len(ids) != 1:
            raise RuntimeError("Expected exactly one container for each shared service")
        container = json.loads(run(["docker", "inspect", ids[0]]))[0]
        address = str(ipaddress.IPv4Address(container["NetworkSettings"]["Networks"][network]["IPAddress"]))
        target = f"{address}:{backend_port}"
        current = active.get(str(port), {}).get("TCPForward")
        if current is not None and current not in (old.get(str(port)), target):
            raise RuntimeError("Refusing to replace an unrelated tailnet forward")
        desired[str(port)] = target
    # Validate every listener before modifying any; preserve all unrelated Serve config.
    for port, target in desired.items():
        if active.get(port, {}).get("TCPForward") != target:
            run(["tailscale", "serve", "--bg", "--yes", f"--tcp={port}", f"tcp://{target}"])
    temporary = STATE.with_suffix(".tmp")
    temporary.write_text(json.dumps(desired) + "\n")
    temporary.replace(STATE)


if __name__ == "__main__":
    main()
