BEGIN;
SELECT pg_advisory_xact_lock(23950211);
CREATE SCHEMA IF NOT EXISTS journal;
CREATE TABLE IF NOT EXISTS journal.schema_migrations (
    version integer PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
SELECT NOT EXISTS (SELECT FROM journal.schema_migrations WHERE version = 1) AS apply \gset
\if :apply
CREATE TABLE journal.daily (
    day date PRIMARY KEY,
    rating integer CHECK (rating BETWEEN 0 AND 10),
    characters integer NOT NULL CHECK (characters >= 0),
    links integer NOT NULL CHECK (links >= 0)
);
CREATE TABLE journal.gym (
    day date PRIMARY KEY
);
CREATE TABLE journal.people_pairs (
    day date NOT NULL,
    first_id text NOT NULL,
    first_name text NOT NULL,
    second_id text NOT NULL,
    second_name text NOT NULL,
    PRIMARY KEY (day, first_id, second_id),
    CHECK (first_id < second_id)
);
INSERT INTO journal.schema_migrations (version) VALUES (1);
\endif
GRANT USAGE ON SCHEMA journal TO journal_grafana;
GRANT SELECT ON journal.daily, journal.gym, journal.people_pairs TO journal_grafana;
COMMIT;
