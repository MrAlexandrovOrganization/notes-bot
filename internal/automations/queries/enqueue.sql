INSERT INTO automation_executions(rule_id,source,event_id,user_id,text,due_at,expires_at,next_attempt_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$6) ON CONFLICT(rule_id,source,event_id) DO NOTHING
