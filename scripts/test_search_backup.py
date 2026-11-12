import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

import notes_backup
import search_backup


class SearchBackupTests(unittest.TestCase):
    def test_independent_archive_verifies_dump_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "index.tar"
            data = b"synthetic pg_dump, never user notes"
            manifest = {"format": 1, "kind": "notes-search",
                        "database": {"image": "pgvector/pgvector@sha256:" + "a" * 64},
                        "sha256": search_backup.digest(io.BytesIO(data))}
            def write(payload):
                with tarfile.open(path, "w") as archive:
                    for name, body in {"search.dump": payload, "manifest.json": json.dumps(manifest).encode()}.items():
                        info = tarfile.TarInfo(name); info.size = len(body)
                        archive.addfile(info, io.BytesIO(body))
            write(data)
            self.assertEqual(search_backup.validate(path), manifest)
            write(data + b"corrupt")
            with self.assertRaises(ValueError):
                search_backup.validate(path)

    def test_core_only_format_does_not_require_a_search_database(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "core.tar"
            files = {name: b"synthetic" for name in notes_backup.MEMBERS - {"manifest.json", "search.dump"}}
            manifest = {"format": 2, "search_location": "external",
                        "databases": {"notifications": {"image": "postgres@sha256:" + "a" * 64}},
                        "sha256": {name: search_backup.digest(io.BytesIO(body)) for name, body in files.items()}}
            with tarfile.open(path, "w") as archive:
                for name, body in {**files, "manifest.json": json.dumps(manifest).encode()}.items():
                    info = tarfile.TarInfo(name); info.size = len(body)
                    archive.addfile(info, io.BytesIO(body))
            self.assertEqual(set(notes_backup.validate(path)["databases"]), {"notifications"})
