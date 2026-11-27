UPDATE {{jobs}}
SET status = $3, audio = CASE WHEN $4 <> '' OR $3 = 'failed' THEN NULL ELSE audio END,
    remote_id = $4, transcript = $5, error_message = $6, result_ready = $7,
    lease_token = NULL, lease_until = NULL, next_at = now() + $8::interval
WHERE id = $1 AND lease_token = $2 AND lease_until > now();
