ALTER TABLE english_review_session ADD COLUMN card_visibility jsonb NOT NULL DEFAULT '{}';

CREATE TABLE english_visibility_operations (
    operation_id text PRIMARY KEY,
    card_id text NOT NULL,
    hidden boolean NOT NULL
);
