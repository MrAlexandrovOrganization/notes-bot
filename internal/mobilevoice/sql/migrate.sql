CREATE TABLE IF NOT EXISTS mobile_voice_schema_migrations (
    version integer PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
