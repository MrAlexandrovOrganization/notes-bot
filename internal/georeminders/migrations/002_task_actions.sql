ALTER TABLE geo_rules ADD COLUMN create_task BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE geo_task_executions (LIKE geo_deliveries INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE geo_task_executions
  DROP COLUMN place_name,
  DROP COLUMN transition,
  ADD COLUMN task_date TEXT NOT NULL,
  ADD PRIMARY KEY (id),
  ADD FOREIGN KEY (rule_id) REFERENCES geo_rules(id),
  ADD UNIQUE (rule_id,event_sequence);
CREATE INDEX geo_task_executions_due ON geo_task_executions(next_attempt_at,id) WHERE status IN ('pending','sending');
