FROM python:3.11-slim AS proto-builder

RUN pip install --no-cache-dir grpcio-tools==1.62.3

WORKDIR /proto-build
COPY proto/ ./proto/

RUN python -m grpc_tools.protoc \
        -I proto \
        --python_out=proto \
        --grpc_python_out=proto \
        proto/notes.proto && \
    sed -i 's/^import notes_pb2/from proto import notes_pb2/' proto/notes_pb2_grpc.py


FROM python:3.11-slim

RUN pip install --no-cache-dir poetry==1.7.1

WORKDIR /app

COPY pyproject.toml poetry.lock* ./

RUN poetry config virtualenvs.create false && \
    poetry install --only main --no-interaction --no-ansi --no-root

COPY frontends/ ./frontends/
COPY --from=proto-builder /proto-build/proto/ ./proto/
COPY main.py .

CMD ["python", "main.py"]
