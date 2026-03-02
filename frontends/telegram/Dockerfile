FROM python:3.11-slim

RUN pip install --no-cache-dir poetry==1.7.1

WORKDIR /app

COPY pyproject.toml poetry.lock* ./

RUN poetry config virtualenvs.create false

RUN poetry install --only main --no-interaction --no-ansi --no-root

COPY frontends/ ./frontends/
COPY proto/ ./proto/
COPY main.py .

RUN pip install --no-cache-dir --retries 5 --timeout 120 grpcio-tools && \
    python -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto proto/notes.proto && \
    sed -i 's/^import notes_pb2/from proto import notes_pb2/' proto/notes_pb2_grpc.py && \
    pip uninstall -y grpcio-tools

CMD ["python", "main.py"]
