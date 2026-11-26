SELECT id::text, status, error_message FROM mobile_voice_jobs WHERE id = $1;
