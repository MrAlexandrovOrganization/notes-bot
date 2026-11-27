INSERT INTO geo_task_executions(rule_id,event_sequence,user_id,title,expires_at,task_date)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(rule_id,event_sequence) DO NOTHING
