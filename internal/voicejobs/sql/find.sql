SELECT payload,cancel_requested FROM voice_jobs WHERE chat_id=$1 AND message_id=$2;
