UPDATE voice_jobs SET cancel_requested=TRUE,next_at=now()
WHERE id=$1 AND user_id=$2 AND NOT done;
