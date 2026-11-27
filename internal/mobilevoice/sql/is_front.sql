SELECT NOT EXISTS (
    SELECT 1 FROM (
        SELECT note_date, note_path, accepted_order FROM mobile_voice_jobs WHERE status IN ('queued', 'transcribing')
        UNION ALL
        SELECT note_date, note_path, accepted_order FROM web_voice_jobs WHERE status IN ('queued', 'transcribing')
    ) AS earlier
    WHERE earlier.accepted_order < job.accepted_order
        AND COALESCE(NULLIF(earlier.note_path, ''), 'Daily/' || earlier.note_date || '.md')
            = COALESCE(NULLIF(job.note_path, ''), 'Daily/' || job.note_date || '.md')
)
FROM {{jobs}} AS job
WHERE job.id = $1 AND job.lease_token = $2 AND job.lease_until > now();
