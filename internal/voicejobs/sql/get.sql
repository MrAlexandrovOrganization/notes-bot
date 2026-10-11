SELECT payload,cancel_requested FROM voice_jobs WHERE id=$1 AND user_id=$2;
