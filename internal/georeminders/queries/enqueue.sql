INSERT INTO geo_deliveries(rule_id,event_sequence,user_id,title,place_name,transition,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(rule_id,event_sequence) DO NOTHING
