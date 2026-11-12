SELECT COALESCE(EXTRACT(epoch FROM (now()-min(e.due_at))),0)::double precision
FROM automation_executions e JOIN automation_rules r ON r.id=e.rule_id
WHERE e.status IN ('pending','sending') AND r.enabled AND e.due_at<now() AND e.expires_at>now()
