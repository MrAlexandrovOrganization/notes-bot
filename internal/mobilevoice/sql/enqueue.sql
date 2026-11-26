INSERT INTO mobile_voice_jobs (id, note_date, note_path, audio_hash, audio)
SELECT $1, $2, $3, $4, $5
WHERE (SELECT count(*) FROM mobile_voice_jobs WHERE status IN ('queued', 'transcribing')) < 16
RETURNING id::text, status, error_message;
