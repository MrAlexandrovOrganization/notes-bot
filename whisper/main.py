"""Entry point for the Whisper transcription gRPC service."""

import logging
import os
from concurrent import futures

import grpc

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
    whisper_pb2_grpc.add_TranscriptionServiceServicer_to_server(
        TranscriptionServicer(), server
    )
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Whisper gRPC server started on port {port}")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
