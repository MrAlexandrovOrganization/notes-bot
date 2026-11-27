SELECT id::text, status, error_message, note_date, note_path, audio_hash
FROM {{jobs}} WHERE id = $1;
