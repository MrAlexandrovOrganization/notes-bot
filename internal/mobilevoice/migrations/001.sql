CREATE TABLE mobile_voice_jobs (
    id uuid PRIMARY KEY,
    note_date text NOT NULL,
    note_path text NOT NULL,
    audio_hash bytea NOT NULL CHECK (octet_length(audio_hash) = 32),
    audio bytea CHECK (octet_length(audio) BETWEEN 1 AND 16777216),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'transcribing', 'saved', 'failed')),
    remote_id text NOT NULL DEFAULT '',
    transcript text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    result_ready boolean NOT NULL DEFAULT false,
    next_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_until timestamptz,
    CHECK ((note_date = '') <> (note_path = '')),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);

CREATE INDEX mobile_voice_jobs_pending
    ON mobile_voice_jobs (next_at, id)
    WHERE status IN ('queued', 'transcribing');
