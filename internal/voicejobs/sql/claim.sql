WITH candidate AS (
 SELECT id FROM voice_jobs WHERE NOT done AND next_at<=now()
 AND (lease_until IS NULL OR lease_until<now())
 ORDER BY next_at,created_at, message_id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE voice_jobs j SET lease_token=$1,lease_until=now()+interval '240 seconds'
FROM candidate c WHERE j.id=c.id RETURNING j.payload,j.cancel_requested;
