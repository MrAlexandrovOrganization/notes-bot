CREATE SEQUENCE voice_accept_order_seq;

ALTER TABLE mobile_voice_jobs ADD COLUMN accepted_order bigint;
ALTER TABLE web_voice_jobs ADD COLUMN accepted_order bigint;

-- Previously accepted jobs have no immutable creation time. Use their
-- persisted schedule to give them a deterministic order ahead of new jobs.
WITH backlog AS (
    SELECT id, false AS web, next_at FROM mobile_voice_jobs WHERE status IN ('queued', 'transcribing')
    UNION ALL
    SELECT id, true AS web, next_at FROM web_voice_jobs WHERE status IN ('queued', 'transcribing')
),
ordered AS MATERIALIZED (
    SELECT id, web, nextval('voice_accept_order_seq') AS accepted_order
    FROM backlog ORDER BY next_at, web, id
),
mobile AS (
    UPDATE mobile_voice_jobs AS job SET accepted_order = ordered.accepted_order
    FROM ordered WHERE NOT ordered.web AND job.id = ordered.id
)
UPDATE web_voice_jobs AS job SET accepted_order = ordered.accepted_order
FROM ordered WHERE ordered.web AND job.id = ordered.id;

-- Finished jobs do not participate in delivery order.
ALTER TABLE mobile_voice_jobs
    ALTER COLUMN accepted_order SET DEFAULT nextval('voice_accept_order_seq'),
    ADD CONSTRAINT mobile_voice_jobs_pending_order CHECK (status IN ('saved', 'failed') OR accepted_order IS NOT NULL);
ALTER TABLE web_voice_jobs
    ALTER COLUMN accepted_order SET DEFAULT nextval('voice_accept_order_seq'),
    ADD CONSTRAINT web_voice_jobs_pending_order CHECK (status IN ('saved', 'failed') OR accepted_order IS NOT NULL);

CREATE INDEX mobile_voice_jobs_delivery_order
    ON mobile_voice_jobs (accepted_order) WHERE status IN ('queued', 'transcribing');
CREATE INDEX web_voice_jobs_delivery_order
    ON web_voice_jobs (accepted_order) WHERE status IN ('queued', 'transcribing');

INSERT INTO voice_order_schema_migrations (version) VALUES (3);
