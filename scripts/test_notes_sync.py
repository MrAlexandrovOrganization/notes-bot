"""Safety checks use only synthetic files and never contact Yandex or production."""

from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from notes_sync import Settings, execute


class NotesSyncTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.settings = Settings(root / "rclone", root / "vault", "synthetic:notes",
                                 root / "private.conf", root / "state", root / "backups",
                                 "synthetic:backups", root / "filters")
        self.settings.local.mkdir()
        (self.settings.local / ".notes-sync-access").write_text("synthetic access marker")
        self.settings.config.write_text("synthetic credentials")
        self.settings.config.chmod(0o600)
        self.settings.filters.write_text("- /.git/**\n")
        self.settings.filters.chmod(0o444)

    def test_missing_mount_marker_stops_before_any_external_command(self):
        (self.settings.local / ".notes-sync-access").unlink()
        with patch("notes_sync.subprocess.run") as run, patch("notes_sync.os.execv") as execute_process:
            with self.assertRaisesRegex(RuntimeError, "access marker"):
                execute(self.settings, "run")
            run.assert_not_called()
            execute_process.assert_not_called()

    def test_exposed_credentials_stop_before_any_external_command(self):
        self.settings.config.chmod(0o644)
        with patch("notes_sync.os.execv") as execute_process:
            with self.assertRaisesRegex(RuntimeError, "0600"):
                execute(self.settings, "run")
            execute_process.assert_not_called()

    def test_bootstrap_refuses_conflicting_data(self):
        with patch("notes_sync.subprocess.run", return_value=subprocess.CompletedProcess([], 1)), \
                patch("notes_sync.os.execv") as execute_process:
            self.assertEqual(execute(self.settings, "bootstrap"), 1)
            execute_process.assert_not_called()

    def test_regular_run_never_reinitializes_baseline_and_retains_both_backups(self):
        with patch("notes_sync.subprocess.run") as run, patch("notes_sync.os.execv") as execute_process:
            execute(self.settings, "run")
            run.assert_not_called()
            command = execute_process.call_args.args[1]
            self.assertNotIn("--resync", command)
            self.assertIn("--backup-dir1", command)
            self.assertIn("--backup-dir2", command)
            self.assertIn("--suffix", command)
            working_filters = Path(command[command.index("--filters-file") + 1])
            self.assertEqual(working_filters.parent, self.settings.state)
            self.assertEqual(working_filters.read_bytes(), self.settings.filters.read_bytes())


if __name__ == "__main__":
    unittest.main()
