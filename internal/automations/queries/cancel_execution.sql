WITH notification AS (
 UPDATE automation_executions SET status='cancelled'
 WHERE id=$1 AND user_id=$2 AND status='pending' AND attempts=0 AND expires_at>now()
 RETURNING id
), task AS (
 UPDATE automation_task_executions SET status='cancelled'
 WHERE id=$1 AND user_id=$2 AND status='pending' AND attempts=0 AND expires_at>now()
 RETURNING id
)
SELECT EXISTS (SELECT 1 FROM notification UNION ALL SELECT 1 FROM task)
