UPDATE geo_deliveries SET status=CASE WHEN $4 THEN 'sent' ELSE 'pending' END,
sent_at=CASE WHEN $4 THEN now() ELSE NULL END,lease_token='',lease_until=NULL,
next_attempt_at=now()+make_interval(secs=>LEAST(300,5*attempts))
WHERE id=$1 AND user_id=$2 AND lease_token=$3 AND status='sending' AND lease_until>now()
