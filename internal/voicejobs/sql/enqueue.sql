INSERT INTO voice_jobs(id,chat_id,message_id,user_id,payload)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(chat_id,message_id) DO NOTHING;
