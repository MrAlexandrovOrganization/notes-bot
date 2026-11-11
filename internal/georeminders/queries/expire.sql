UPDATE geo_deliveries d SET status=CASE WHEN NOT r.enabled THEN 'cancelled' ELSE 'expired' END,
lease_token='',lease_until=NULL
FROM geo_rules r WHERE r.id=d.rule_id AND d.status IN ('pending','sending')
AND (d.expires_at<=now() OR NOT r.enabled)
