"""Explicit bootstrap and guarded, version-preserving Yandex Disk bisync."""

import argparse
from dataclasses import dataclass
import datetime as dt
import os
from pathlib import Path
import stat
import subprocess
import uuid


@dataclass(frozen=True)
class Settings:
    binary: Path
    local: Path
    remote: str
    config: Path
    state: Path
    local_backups: Path
    remote_backups: str
    filters: Path


def validate(settings):
    if settings.config.is_symlink() or not settings.config.is_file():
        raise RuntimeError("Private rclone configuration is missing or is a symlink")
    metadata = settings.config.stat()
    if metadata.st_uid != os.getuid() or stat.S_IMODE(metadata.st_mode) & 0o077:
        raise RuntimeError("Rclone configuration must belong to this user and have mode 0600")
    if settings.local.is_symlink() or not settings.local.is_dir():
        raise RuntimeError("Expected a real local vault directory")
    if not (settings.local / ".notes-sync-access").is_file():
        raise RuntimeError("Local vault access marker missing; refusing synchronization")


def execute(settings, mode):
    validate(settings)
    common = ["--config", str(settings.config), "--contimeout", "10s", "--timeout", "90s",
              "--retries", "1", "--checkers", "8", "--transfers", "2", "--checksum", "--stats", "0"]
    check = [str(settings.binary), "check", str(settings.local), settings.remote,
             "--filter-from", str(settings.filters), *common]
    # Never choose a winner implicitly when establishing the initial baseline.
    # Bootstrap only after a complete, read-only checksum comparison passes.
    if mode in {"check", "bootstrap", "bootstrap-preview"}:
        result = subprocess.run(check, check=False)
        if result.returncode or mode == "check":
            return result.returncode
    # Bisync writes <filters-file>.md5. Keep the canonical policy root-owned,
    # and its working copy/sidecar inside the user's persistent state directory.
    settings.state.mkdir(parents=True, exist_ok=True, mode=0o700)
    if settings.state.is_symlink():
        raise RuntimeError("Synchronization state must not be a symlink")
    private_filters = settings.state / "filters.txt"
    if private_filters.is_symlink():
        raise RuntimeError("Working filters must not be a symlink")
    policy = settings.filters.read_bytes()
    if not private_filters.exists() or private_filters.read_bytes() != policy:
        private_filters.write_bytes(policy)
        private_filters.chmod(0o600)
    suffix = dt.datetime.now(dt.UTC).strftime(".%Y%m%dT%H%M%S") + "." + uuid.uuid4().hex[:8]
    command = [str(settings.binary), "bisync", str(settings.local), settings.remote,
               "--workdir", str(settings.state), "--filters-file", str(private_filters),
               "--compare", "size,checksum", "--check-access", "--check-filename", ".notes-sync-access",
               "--max-delete", "1", "--max-lock", "15m", "--resilient", "--recover",
               "--backup-dir1", str(settings.local_backups), "--backup-dir2", settings.remote_backups,
               "--suffix", suffix, "--create-empty-src-dirs", *common]
    if mode in {"bootstrap", "bootstrap-preview"}:
        command += ["--resync", "--resync-mode", "path1"]
    if mode == "bootstrap-preview":
        command += ["--dry-run"]
    # Replace the wrapper so systemd's SIGINT reaches rclone's graceful shutdown.
    os.execv(str(settings.binary), command)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["run", "check", "bootstrap", "bootstrap-preview"])
    args = parser.parse_args()
    home = Path.home()
    if args.mode != "check" and subprocess.run(
        ["systemctl", "is-active", "--quiet", "yandex-disk.service"], check=False
    ).returncode == 0:
        raise RuntimeError("Stop the legacy Yandex Disk client before bisync")
    return execute(Settings(
        binary=Path("/usr/local/lib/notes-sync/rclone"),
        local=home / "Yandex.Disk/notes", remote="notes-yandex:notes",
        config=home / ".config/rclone/notes.conf", state=home / ".local/state/notes-bisync",
        local_backups=home / ".local/state/notes-bisync-backups",
        remote_backups="notes-yandex:notes-sync-backups/russia",
        filters=Path("/usr/local/lib/notes-sync/filters.txt"),
    ), args.mode)


if __name__ == "__main__":
    os.umask(0o077)
    raise SystemExit(main())
