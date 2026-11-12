SELECT id,payload,enabled AND (repeat OR last_fired_at IS NULL)
FROM automation_rules WHERE user_id=$1 AND enabled ORDER BY id DESC
