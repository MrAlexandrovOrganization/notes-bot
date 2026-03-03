FROM python:3.11-slim AS proto-builder

RUN pip install --no-cache-dir grpcio-tools==1.62.3

WORKDIR /proto-build
COPY proto/ ./proto/

RUN python -m grpc_tools.protoc \
        -I proto \
        --python_out=proto \
        --grpc_python_out=proto \
        proto/notifications.proto && \
    sed -i 's/^import notifications_pb2/from proto import notifications_pb2/' proto/notifications_pb2_grpc.py


FROM python:3.11-slim

RUN pip install --no-cache-dir poetry==1.8.3

WORKDIR /app

COPY pyproject.toml poetry.lock* ./

RUN poetry config virtualenvs.create false && \
    poetry install --only main --no-interaction --no-ansi --no-root

COPY notifications/ ./notifications/
COPY --from=proto-builder /proto-build/proto/ ./proto/

CMD ["python", "-m", "notifications.main"]
