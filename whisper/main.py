"""Entry point for the Whisper transcription gRPC service."""

import logging
import os
from concurrent import futures

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from proto import whisper_pb2_grpc
from whisper.server import TranscriptionServicer

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s", level=logging.INFO
)
logger = logging.getLogger(__name__)

_50MB = 50 * 1024 * 1024


def serve():
    port = os.getenv("GRPC_PORT", "50053")
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=4),
        options=[
            ("grpc.max_receive_message_length", _50MB),
            ("grpc.max_send_message_length", _50MB),
        ],
    )

    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)

    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Whisper gRPC server started on port {port}, loading model...")

    # Model loading happens here — container is NOT_SERVING until complete.
    servicer = TranscriptionServicer()
    whisper_pb2_grpc.add_TranscriptionServiceServicer_to_server(servicer, server)

    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)
    logger.info("Whisper service ready.")

    server.wait_for_termination()


if __name__ == "__main__":
    serve()
