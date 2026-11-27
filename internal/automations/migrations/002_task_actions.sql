CREATE TABLE automation_task_executions (LIKE automation_executions INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE automation_task_executions
  ADD COLUMN task_date TEXT NOT NULL,
  ADD PRIMARY KEY (id),
  ADD FOREIGN KEY (rule_id) REFERENCES automation_rules(id),
  ADD FOREIGN KEY (source,event_id) REFERENCES automation_events(source,id),
  ADD UNIQUE (rule_id,source,event_id);
CREATE INDEX automation_task_executions_due ON automation_task_executions(next_attempt_at,id) WHERE status IN ('pending','sending');
