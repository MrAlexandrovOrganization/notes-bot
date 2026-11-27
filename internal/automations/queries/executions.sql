WITH actions AS (
 SELECT id,rule_id,user_id,status,attempts,due_at,'notify' AS action_type FROM automation_executions
 WHERE user_id=$1
 UNION ALL
 SELECT id,rule_id,user_id,status,attempts,due_at,'create_task' FROM automation_task_executions
 WHERE user_id=$1
)
SELECT e.id,e.rule_id,e.status,e.attempts,e.due_at,r.payload,e.action_type
FROM actions e JOIN automation_rules r ON r.id=e.rule_id
ORDER BY e.id DESC LIMIT 50
