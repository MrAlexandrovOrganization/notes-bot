CREATE TABLE english_review_session (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    state jsonb NOT NULL DEFAULT '{}',
    legacy_imported boolean NOT NULL DEFAULT false
);

INSERT INTO english_review_session DEFAULT VALUES;

CREATE TABLE english_review_history (
    card_id text NOT NULL,
    token text NOT NULL,
    action text NOT NULL CHECK (action IN ('miss', 'hard', 'easy', 'skip')),
    reviewed_at timestamptz NOT NULL,
    skipped_rounds integer NOT NULL CHECK (skipped_rounds BETWEEN 0 AND 16),
    round_no bigint NOT NULL CHECK (round_no >= 0),
    PRIMARY KEY (card_id, token)
);

CREATE UNIQUE INDEX english_round_attempt ON english_review_history (token)
    WHERE round_no > 0;
