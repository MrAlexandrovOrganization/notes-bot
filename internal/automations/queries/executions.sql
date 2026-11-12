SELECT e.id,e.rule_id,e.status,e.attempts,e.due_at,r.payload
FROM automation_executions e JOIN automation_rules r ON r.id=e.rule_id
WHERE e.user_id=$1 ORDER BY e.id DESC LIMIT 50
