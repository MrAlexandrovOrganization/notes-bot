SELECT id,user_id,payload,delay_seconds FROM automation_rules
WHERE enabled AND event_type=$1 AND subject=$2 AND target_status=$3 AND created_at<=$4
AND (repeat OR last_fired_at IS NULL) ORDER BY id FOR UPDATE
