WITH next AS (
 SELECT d.id,r.request_id FROM geo_task_executions d JOIN geo_rules r ON r.id=d.rule_id
 WHERE d.user_id=$1 AND r.enabled AND d.expires_at>now()
 AND (d.status='pending' OR (d.status='sending' AND d.lease_until<=now()))
 AND d.next_attempt_at<=now()
 ORDER BY d.id FOR UPDATE OF d SKIP LOCKED LIMIT 1
)
UPDATE geo_task_executions d SET status='sending',lease_token=$2,lease_until=now()+interval '45 seconds',attempts=attempts+1
FROM next WHERE d.id=next.id
RETURNING d.id,d.lease_token,d.title,d.expires_at,d.task_date,next.request_id::text || ':' || d.event_sequence::text
