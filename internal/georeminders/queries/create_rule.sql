INSERT INTO geo_rules(user_id,place_id,title,transition,repeat,max_age_seconds,timezone,request_id)
SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE (SELECT count(*) FROM geo_rules WHERE user_id=$1 AND enabled)<100
OR EXISTS(SELECT 1 FROM geo_rules WHERE user_id=$1 AND request_id=$8)
ON CONFLICT(user_id,request_id) DO UPDATE SET request_id=geo_rules.request_id
WHERE geo_rules.place_id=EXCLUDED.place_id AND geo_rules.title=EXCLUDED.title
AND geo_rules.transition=EXCLUDED.transition AND geo_rules.repeat=EXCLUDED.repeat
AND geo_rules.max_age_seconds=EXCLUDED.max_age_seconds AND geo_rules.timezone=EXCLUDED.timezone
RETURNING id,enabled AND (repeat<>'once' OR last_fired_at IS NULL),last_fired_at
