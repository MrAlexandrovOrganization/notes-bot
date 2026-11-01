"""Synthetic backup integrity and failure tests; never access production hosts."""

import datetime as dt
import io
import fcntl
import os
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
import subprocess
from unittest.mock import patch

import notes_backup as backup
import yandex_disk


class NotesBackupTests(unittest.TestCase):
    def test_backup_and_deploy_are_mutually_exclusive(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            with backup.deployment_lock(home):
                with (home / ".notes-bot-images-deploy.lock").open("a") as deploy:
                    with self.assertRaises(BlockingIOError):
                        fcntl.flock(deploy, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with (home / ".notes-bot-images-deploy.lock").open("a") as deploy:
                fcntl.flock(deploy, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaisesRegex(backup.BackupError, "Deployment in progress"):
                    with backup.deployment_lock(home):
                        self.fail("Backup must not start during deployment")
            self.assertEqual((home / ".notes-bot-images-deploy.lock").stat().st_uid, os.getuid())

    def test_backup_lock_rejects_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            other = home / "unrelated"
            other.write_text("preserve")
            (home / ".notes-bot-images-deploy.lock").symlink_to(other)
            with self.assertRaises(OSError):
                with backup.deployment_lock(home):
                    self.fail("Symlink lock must be rejected")
            self.assertEqual(other.read_text(), "preserve")

    def test_vault_roundtrip_includes_git_ignored_unicode_and_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "source"
            (root / ".git/objects").mkdir(parents=True)
            (root / ".git/objects/synthetic").write_bytes(b"git history")
            (root / ".obsidian").mkdir()
            (root / ".obsidian/ignored.json").write_text("{}")
            (root / "Заметка.md").write_text("Незакоммиченный текст <>&\n")
            (root / "link").symlink_to("Заметка.md")
            archive = Path(directory) / "vault.tgz"
            manifest = backup.snapshot_vault(root, archive)
            restored = Path(directory) / "restored"
            restored.mkdir()
            with tarfile.open(archive) as stream:
                stream.extractall(restored, filter="data")
            self.assertEqual(backup.inventory(root), backup.inventory(restored))
            self.assertEqual(manifest["sha256"], backup.fingerprint(backup.inventory(restored)))

    def test_concurrent_writes_never_publish_mixed_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "source"
            root.mkdir()
            (root / "note").write_text("original")
            with patch.object(backup, "archive_inventory", return_value={}):
                with self.assertRaises(backup.BackupError):
                    backup.snapshot_vault(root, Path(directory) / "vault.tgz")

    def test_archive_rejects_path_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "vault.tgz"
            with tarfile.open(path, "w:gz") as archive:
                entry = tarfile.TarInfo("../outside")
                entry.size = 1
                archive.addfile(entry, io.BytesIO(b"x"))
            with self.assertRaises(backup.BackupError):
                backup.archive_inventory(path)

    def test_outer_archive_checks_payload_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "backup.tar"
            manifest = {"format": 1, "databases": {k: {"image": "postgres@sha256:" + "a" * 64}
                                                   for k in ("search", "notifications")},
                        "sha256": {k: "0" * 64 for k in backup.MEMBERS - {"manifest.json"}}}
            with tarfile.open(path, "w:") as archive:
                for name in backup.MEMBERS:
                    data = json.dumps(manifest).encode() if name == "manifest.json" else b"corrupt"
                    entry = tarfile.TarInfo(name)
                    entry.size = len(data)
                    archive.addfile(entry, io.BytesIO(data))
            with self.assertRaisesRegex(backup.BackupError, "checksum mismatch"):
                backup.validate(path)

    def test_failed_pull_preserves_old_backups_and_latest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            old = root / ("notes-bot-20260101T000000Z-" + "a" * 32 + ".tar")
            old.write_bytes(b"precious")
            (root / "latest.json").write_text("unchanged")
            config = {"directory": directory, "identity": "synthetic", "known_hosts": "synthetic",
                      "source": "synthetic"}
            with patch.object(backup, "run", side_effect=backup.BackupError("offline")):
                with self.assertRaises(backup.BackupError):
                    backup.pull(config)
            self.assertEqual(old.read_bytes(), b"precious")
            self.assertEqual((root / "latest.json").read_text(), "unchanged")
            self.assertFalse(list(root.glob("*.partial")))

    def test_retention_keeps_recent_and_monthly_copies(self):
        now = dt.datetime(2026, 10, 31, tzinfo=dt.UTC)
        recent = Path("notes-bot-20261030T230000Z-" + "a" * 32 + ".tar")
        old = Path("notes-bot-20260101T000000Z-" + "b" * 32 + ".tar")
        self.assertEqual(backup.retained([recent, old, Path("unrelated.tar")], now), {recent, old})

    def test_empty_or_wrong_database_is_rejected(self):
        with patch.object(backup, "run", return_value=b'[]'):
            with self.assertRaises(backup.BackupError):
                backup.check_tables("synthetic", "notifications")
        tables = list(backup.REQUIRED_TABLES["search"]) + ["future_table"]
        with patch.object(backup, "run", return_value=json.dumps(tables).encode()):
            backup.check_tables("synthetic", "search")

    def test_imported_image_cache_cannot_substitute_other_content(self):
        metadata = {"Architecture": "amd64", "Os": "linux", "Config": {}, "RootFS": {"Layers": ["a"]}}
        config = {"image_cache": {"postgres@sha256:" + "a" * 64:
                  {"reference": "notes-backup-cache:synthetic", "fingerprint": "wrong"}}}
        responses = [subprocess.CompletedProcess([], 1, b''),
                     subprocess.CompletedProcess([], 0, json.dumps([metadata]).encode())]
        with patch.object(backup.subprocess, "run", side_effect=responses):
            with self.assertRaisesRegex(backup.BackupError, "differs"):
                backup.restore_image({"image": "postgres@sha256:" + "a" * 64}, config)

    def test_foreground_sync_keeps_exclusions_and_auth_without_shell_evaluation(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.cfg"
            config.write_text('dir="/synthetic/Yandex.Disk"\nexclude-dirs="Фото,Книги с пробелами"\n'
                              'auth="/synthetic/passwd"\nproxy="no"\nread-only="no"\n')
            args = yandex_disk.arguments(config, Path("/synthetic"))
            self.assertIn("--exclude-dirs=Фото,Книги с пробелами", args)
            self.assertIn("--auth=/synthetic/passwd", args)
            self.assertIn("--proxy=no", args)
            self.assertNotIn("--read-only", args)
            self.assertNotIn("start", args)

    def test_foreground_sync_rejects_unknown_configuration_instead_of_dropping_it(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.cfg"
            config.write_text('dir="/synthetic"\nunknown="setting"\n')
            with self.assertRaises(RuntimeError):
                yandex_disk.arguments(config, Path("/synthetic"))


if __name__ == "__main__":
    unittest.main()
