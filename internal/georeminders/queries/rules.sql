SELECT id,place_id::text,title,transition,repeat,max_age_seconds,timezone,
       (repeat<>'once' OR last_fired_at IS NULL),last_fired_at
FROM geo_rules WHERE user_id=$1 AND enabled ORDER BY id
