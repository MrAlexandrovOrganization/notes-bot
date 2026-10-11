SELECT NOT EXISTS(SELECT 1 FROM voice_jobs
 WHERE chat_id=$1 AND message_id<$2 AND NOT done);
