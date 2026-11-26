DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'notes_english_grafana') THEN
        CREATE ROLE notes_english_grafana LOGIN CONNECTION LIMIT 3;
    END IF;
END
$$;

ALTER ROLE notes_english_grafana SET default_transaction_read_only TO on;
ALTER ROLE notes_english_grafana SET statement_timeout TO '10s';
GRANT SELECT ON english_report_events, english_report_sessions TO notes_english_grafana;
