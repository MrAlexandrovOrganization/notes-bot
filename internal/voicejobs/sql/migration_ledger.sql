CREATE TABLE IF NOT EXISTS voice_schema_migrations(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
