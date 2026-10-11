CREATE TABLE voice_jobs (
    id TEXT PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    payload JSONB NOT NULL,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    done BOOLEAN NOT NULL DEFAULT FALSE,
    next_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token TEXT,
    lease_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(chat_id, message_id)
);
CREATE INDEX voice_jobs_pending ON voice_jobs(next_at) WHERE NOT done;
