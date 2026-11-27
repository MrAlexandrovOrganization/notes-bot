SELECT status,count(*) FROM (
 SELECT status FROM automation_executions
 UNION ALL
 SELECT status FROM automation_task_executions
) actions GROUP BY status
