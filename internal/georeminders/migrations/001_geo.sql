CREATE TABLE geo_cursor (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    sequence BIGINT NOT NULL DEFAULT 0
);
INSERT INTO geo_cursor(singleton) VALUES(true);
CREATE TABLE geo_rules (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    request_id UUID NOT NULL,
    place_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 500),
    transition TEXT NOT NULL CHECK (transition IN ('entered','exited')),
    repeat TEXT NOT NULL CHECK (repeat IN ('once','always','daily')),
    max_age_seconds INTEGER NOT NULL CHECK (max_age_seconds BETWEEN 60 AND 3600),
    timezone TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_fired_at TIMESTAMPTZ,
    UNIQUE(user_id,request_id)
);
CREATE INDEX geo_rules_place ON geo_rules(place_id) WHERE enabled;
CREATE TABLE geo_deliveries (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES geo_rules(id),
    event_sequence BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    title TEXT NOT NULL,
    place_name TEXT NOT NULL,
    transition TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','expired','cancelled')),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    UNIQUE(rule_id,event_sequence)
);
CREATE INDEX geo_deliveries_due ON geo_deliveries(next_attempt_at,id) WHERE status IN ('pending','sending');
