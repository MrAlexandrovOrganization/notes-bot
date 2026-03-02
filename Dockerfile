FROM python:3.11-slim

# Install Poetry
RUN pip install --no-cache-dir poetry==1.7.1

WORKDIR /app

# Copy poetry files
COPY pyproject.toml poetry.lock* ./

# Configure poetry to not create virtual environment
RUN poetry config virtualenvs.create false

# Install dependencies
RUN poetry install --no-interaction --no-ansi --no-root

# Copy bot code
COPY core/ ./core/
COPY frontends/ ./frontends/
COPY main.py .

# Run the bot
CMD ["python", "main.py"]