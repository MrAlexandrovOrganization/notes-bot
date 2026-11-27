-- Run as the PostgreSQL instance administrator, connected to postgres.
\getenv writer_password JOURNAL_WRITER_PASSWORD
\getenv reader_password JOURNAL_READER_PASSWORD

SELECT format('CREATE ROLE journal_writer LOGIN CONNECTION LIMIT 2 PASSWORD %L', :'writer_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'journal_writer') \gexec
SELECT format('CREATE ROLE journal_grafana LOGIN CONNECTION LIMIT 3 PASSWORD %L', :'reader_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'journal_grafana') \gexec
SELECT 'CREATE DATABASE journal OWNER journal_writer'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'journal') \gexec

REVOKE ALL ON DATABASE journal FROM PUBLIC;
GRANT CONNECT ON DATABASE journal TO journal_grafana;
ALTER ROLE journal_grafana IN DATABASE journal SET default_transaction_read_only TO on;
ALTER ROLE journal_grafana IN DATABASE journal SET statement_timeout TO '10s';
