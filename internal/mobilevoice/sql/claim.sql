WITH selected AS (
    SELECT id FROM mobile_voice_jobs
    WHERE status IN ('queued', 'transcribing')
        AND next_at <= now()
        AND (lease_until IS NULL OR lease_until <= now())
    ORDER BY next_at, id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE mobile_voice_jobs AS job
SET lease_token = $1, lease_until = now() + interval '150 seconds'
FROM selected
WHERE job.id = selected.id
RETURNING job.id::text, job.status, job.error_message, job.note_date, job.note_path,
          job.audio, job.remote_id, job.transcript, job.result_ready;
