"""Health check for the core gRPC service.

Calls GetExistingDates and exits 0 on success, 1 on failure.
"""

import os
import sys

import grpc

from proto import notes_pb2, notes_pb2_grpc

port = os.getenv("GRPC_PORT", "50051")

try:
    channel = grpc.insecure_channel(f"localhost:{port}")
    stub = notes_pb2_grpc.NotesServiceStub(channel)
    response = stub.GetExistingDates(notes_pb2.Empty(), timeout=5)
    print(f"OK: {len(response.dates)} daily notes found")
    sys.exit(0)
except Exception as e:
    print(f"FAIL: {e}", file=sys.stderr)
    sys.exit(1)
