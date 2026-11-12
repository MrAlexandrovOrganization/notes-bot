UPDATE automation_executions e SET status=CASE WHEN r.enabled THEN 'expired' ELSE 'cancelled' END,
lease_token='',lease_until=NULL
FROM automation_rules r WHERE r.id=e.rule_id AND e.status IN ('pending','sending') AND (e.expires_at<=now() OR NOT r.enabled)
