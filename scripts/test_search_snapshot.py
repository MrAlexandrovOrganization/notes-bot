"""Synthetic files only; no production vault, SSH or cloud sync client."""
import importlib.util
import os
from pathlib import Path
import shutil
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("snapshot", Path(__file__).with_name("search_snapshot.py"))
snapshot = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(snapshot)


class SnapshotTests(unittest.TestCase):
    def test_failed_and_empty_transfers_keep_the_last_complete_generation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "mirror"
            source = Path(directory) / "source"
            source.mkdir()
            (source / "note.md").write_text("first generation")
            config = {"directory": str(root)}

            def copy(_config, target, _previous):
                shutil.copytree(source, target, dirs_exist_ok=True)

            first = snapshot.publish(config, copy)
            original = os.readlink(root / "current")

            def fail(_config, target, _previous):
                (target / "note.md").write_text("partial transfer")
                raise RuntimeError("synthetic connection failure")

            with self.assertRaises(RuntimeError):
                snapshot.publish(config, fail)
            self.assertEqual(os.readlink(root / "current"), original)
            with self.assertRaises(ValueError):
                snapshot.publish(config, lambda *_: None)
            self.assertEqual((root / "current/note.md").read_text(), "first generation")
            (source / "note.md").write_text("second generation")
            second = snapshot.publish(config, copy)
            self.assertNotEqual(first["generation"], second["generation"])
            self.assertEqual((root / "current/note.md").read_text(), "second generation")
            self.assertEqual((root / "generations" / first["generation"] / "note.md").read_text(), "first generation")
            self.assertEqual(snapshot.publish(config, copy)["generation"], second["generation"])

    def test_external_links_cannot_escape_the_replica(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "external.md").write_text("must not be read")
            target = root / "generation"
            target.mkdir()
            (target / "escape.md").symlink_to(root / "external.md")
            with self.assertRaises(ValueError):
                snapshot.fingerprint(target)
