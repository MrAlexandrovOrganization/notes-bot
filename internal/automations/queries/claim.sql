WITH next AS (
 SELECT e.id FROM automation_executions e JOIN automation_rules r ON r.id=e.rule_id
 WHERE e.user_id=$1 AND r.enabled AND e.due_at<=now() AND e.expires_at>now()
 AND (e.status='pending' OR (e.status='sending' AND e.lease_until<=now())) AND e.next_attempt_at<=now()
 ORDER BY e.due_at,e.id FOR UPDATE OF e SKIP LOCKED LIMIT 1
)
UPDATE automation_executions e SET status='sending',lease_token=$2,lease_until=now()+interval '45 seconds',attempts=attempts+1
FROM next WHERE e.id=next.id RETURNING e.id,e.lease_token,e.text,e.expires_at
