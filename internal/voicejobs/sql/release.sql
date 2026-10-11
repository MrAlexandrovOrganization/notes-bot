UPDATE voice_jobs SET payload=$3,done=$4,next_at=now()+($5*interval '1 second'),
lease_token=NULL,lease_until=NULL,updated_at=now()
WHERE id=$1 AND lease_token=$2 AND lease_until>now();
