CREATE TABLE automation_rules (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL,
 request_id UUID NOT NULL,
 payload BYTEA NOT NULL,
 payload_hash BYTEA NOT NULL,
 event_type TEXT NOT NULL,
 subject TEXT NOT NULL,
 target_status INTEGER NOT NULL CHECK(target_status BETWEEN 1 AND 3),
 delay_seconds INTEGER NOT NULL CHECK(delay_seconds BETWEEN 0 AND 2592000),
 repeat BOOLEAN NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT true,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_fired_at TIMESTAMPTZ,
 UNIQUE(user_id,request_id)
);
CREATE INDEX automation_rules_subject ON automation_rules(event_type,subject) WHERE enabled;
CREATE TABLE automation_events (
 source TEXT NOT NULL,
 id UUID NOT NULL,
 subject TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK(revision>0),
 payload_hash BYTEA NOT NULL,
 payload BYTEA NOT NULL,
 occurred_at TIMESTAMPTZ NOT NULL,
 received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(source,id),
 UNIQUE(source,subject,revision)
);
CREATE TABLE automation_executions (
 id BIGSERIAL PRIMARY KEY,
 rule_id BIGINT NOT NULL REFERENCES automation_rules(id),
 source TEXT NOT NULL,
 event_id UUID NOT NULL,
 user_id BIGINT NOT NULL,
 text TEXT NOT NULL,
 due_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','expired','cancelled')),
 lease_token TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL,
 sent_at TIMESTAMPTZ,
 FOREIGN KEY(source,event_id) REFERENCES automation_events(source,id),
 UNIQUE(rule_id,source,event_id)
);
CREATE INDEX automation_executions_due ON automation_executions(next_attempt_at,id) WHERE status IN ('pending','sending');
