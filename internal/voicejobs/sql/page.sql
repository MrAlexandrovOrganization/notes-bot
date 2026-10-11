SELECT payload,cancel_requested FROM voice_jobs
WHERE chat_id=$1 AND user_id=$2 AND (payload->>'status_message_id')::bigint=$3;
