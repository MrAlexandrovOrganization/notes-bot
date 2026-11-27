WITH actions AS (
 SELECT rule_id,status,due_at,expires_at FROM automation_executions
 UNION ALL
 SELECT rule_id,status,due_at,expires_at FROM automation_task_executions
)
SELECT COALESCE(EXTRACT(epoch FROM (now()-min(e.due_at))),0)::double precision
FROM actions e JOIN automation_rules r ON r.id=e.rule_id
WHERE e.status IN ('pending','sending') AND r.enabled AND e.due_at<now() AND e.expires_at>now()
