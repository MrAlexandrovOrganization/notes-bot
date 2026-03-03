"""Entry point for the notifications gRPC service."""

import logging
import os
from concurrent import futures

import grpc

from proto import notifications_pb2_grpc
from notifications.db import ensure_schema
from notifications.scheduler import start_scheduler_thread
from notifications.server import NotificationsServicer

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def serve():
    port = os.getenv("GRPC_PORT", "50052")
    ensure_schema()
    start_scheduler_thread()
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    notifications_pb2_grpc.add_NotificationsServiceServicer_to_server(
        NotificationsServicer(), server
    )
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Notifications gRPC server started on port {port}")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
