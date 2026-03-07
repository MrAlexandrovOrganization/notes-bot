"""Entry point for the notifications gRPC service."""

import logging
import os
from concurrent import futures

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from proto import notifications_pb2_grpc
from notifications.db import ensure_schema
from notifications.scheduler import start_scheduler_thread
from notifications.server import NotificationsServicer

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s", level=logging.INFO
)
logger = logging.getLogger(__name__)


def serve():
    port = os.getenv("GRPC_PORT", "50052")
    ensure_schema()
    start_scheduler_thread()
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    notifications_pb2_grpc.add_NotificationsServiceServicer_to_server(
        NotificationsServicer(), server
    )

    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)

    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Notifications gRPC server started on port {port}")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
