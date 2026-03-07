"""Health check for the notifications gRPC service via grpc.health.v1."""

import os
import sys

import grpc
from grpc_health.v1 import health_pb2, health_pb2_grpc

port = os.getenv("GRPC_PORT", "50052")

try:
    channel = grpc.insecure_channel(f"localhost:{port}")
    stub = health_pb2_grpc.HealthStub(channel)
    response = stub.Check(health_pb2.HealthCheckRequest(service=""), timeout=5)
    if response.status == health_pb2.HealthCheckResponse.SERVING:
        print("OK: service is SERVING")
        sys.exit(0)
    else:
        print(f"FAIL: status={response.status}", file=sys.stderr)
        sys.exit(1)
except Exception as e:
    print(f"FAIL: {e}", file=sys.stderr)
    sys.exit(1)
