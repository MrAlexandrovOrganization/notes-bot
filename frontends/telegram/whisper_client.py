"""gRPC client for the Whisper transcription service."""

import logging
import os

import grpc

from proto import whisper_pb2, whisper_pb2_grpc

logger = logging.getLogger(__name__)

_50MB = 50 * 1024 * 1024

_UNAVAILABLE_CODES = (grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED)


class WhisperUnavailableError(Exception):
    """Raised when the whisper service cannot be reached."""


class WhisperClient:
    def __init__(self, host: str, port: str):
        options = [
            ("grpc.max_receive_message_length", _50MB),
            ("grpc.max_send_message_length", _50MB),
        ]
        self._channel = grpc.insecure_channel(f"{host}:{port}", options=options)
        self._stub = whisper_pb2_grpc.TranscriptionServiceStub(self._channel)

    def transcribe(self, audio_data: bytes, fmt: str) -> str:
        try:
            response = self._stub.Transcribe(
                whisper_pb2.TranscribeRequest(audio_data=audio_data, format=fmt),
                timeout=120,
            )
            return response.text
        except grpc.RpcError as e:
            if e.code() in _UNAVAILABLE_CODES:
                raise WhisperUnavailableError() from e
            raise


whisper_client = WhisperClient(
    os.getenv("WHISPER_GRPC_HOST", "localhost"),
    os.getenv("WHISPER_GRPC_PORT", "50053"),
)
