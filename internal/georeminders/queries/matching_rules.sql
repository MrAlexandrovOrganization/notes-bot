SELECT id,user_id,title,max_age_seconds FROM geo_rules
WHERE enabled AND place_id=$1 AND transition=$2 AND created_at<=$3
AND (repeat<>'once' OR last_fired_at IS NULL)
AND (repeat<>'daily' OR last_fired_at IS NULL OR
     (last_fired_at AT TIME ZONE timezone)::date <> ($4::timestamptz AT TIME ZONE timezone)::date)
FOR UPDATE
