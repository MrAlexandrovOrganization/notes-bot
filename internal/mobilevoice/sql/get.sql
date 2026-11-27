SELECT id::text, status, error_message FROM {{jobs}} WHERE id = $1;
