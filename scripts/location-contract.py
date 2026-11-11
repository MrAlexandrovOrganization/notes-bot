#!/usr/bin/env python3
"""Explicitly sync, or locally verify, the pinned Location contract snapshot."""
import argparse
import hashlib
import json
from pathlib import Path

root = Path(__file__).resolve().parents[1]
snapshot = root / 'proto/location/v1/location.proto'
lock = root / 'proto/location/snapshot.json'
parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path)
parser.add_argument('--revision')
args = parser.parse_args()
if args.source:
    if not args.revision:
        parser.error('--revision is required for an explicit sync')
    data = args.source.read_bytes()
    snapshot.write_bytes(data)
    lock.write_text(json.dumps({
        'source_repository': 'MrAlexandrovOrganization/location',
        'source_path': 'proto/location/v1/location.proto',
        'source_revision': args.revision,
        'sha256': hashlib.sha256(data).hexdigest(),
    }, indent=2) + '\n')
else:
    expected = json.loads(lock.read_text())
    if hashlib.sha256(snapshot.read_bytes()).hexdigest() != expected['sha256']:
        raise SystemExit('Location snapshot differs from its lock; explicitly sync the contract')
