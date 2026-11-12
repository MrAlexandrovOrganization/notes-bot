INSERT INTO automation_rules(user_id,request_id,payload,payload_hash,event_type,subject,target_status,delay_seconds,repeat)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9
WHERE (SELECT count(*) FROM automation_rules WHERE user_id=$1 AND enabled)<100
OR EXISTS(SELECT 1 FROM automation_rules WHERE user_id=$1 AND request_id=$2)
ON CONFLICT(user_id,request_id) DO UPDATE SET request_id=automation_rules.request_id
WHERE automation_rules.payload_hash=EXCLUDED.payload_hash
RETURNING id,enabled AND (repeat OR last_fired_at IS NULL),payload
