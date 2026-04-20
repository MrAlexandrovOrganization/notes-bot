FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY search/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY search/ .

# Expose gRPC port
EXPOSE 50054

# Run the server
CMD ["python", "server.py"]
