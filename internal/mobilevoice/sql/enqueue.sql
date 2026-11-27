INSERT INTO {{jobs}} (id, note_date, note_path, audio_hash, audio)
SELECT $1, $2, $3, $4, $5
WHERE (SELECT count(*) FROM {{jobs}} WHERE status IN ('queued', 'transcribing')) < 16
RETURNING id::text, status, error_message;
