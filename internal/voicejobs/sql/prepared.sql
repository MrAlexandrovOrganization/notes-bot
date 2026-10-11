UPDATE voice_jobs
SET payload=jsonb_set(payload,'{prepared}',$4::jsonb),updated_at=now()
WHERE id=$1 AND user_id=$2 AND done AND payload->'prepared'=$3::jsonb;
