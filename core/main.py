"""Entry point for the core gRPC service."""

import logging
import os
from concurrent import futures

import grpc

from proto import notes_pb2_grpc
from core.server import NotesServicer

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s", level=logging.INFO
)
logger = logging.getLogger(__name__)


def serve():
    port = os.getenv("GRPC_PORT", "50051")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    notes_pb2_grpc.add_NotesServiceServicer_to_server(NotesServicer(), server)
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Core gRPC server started on port {port}")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
