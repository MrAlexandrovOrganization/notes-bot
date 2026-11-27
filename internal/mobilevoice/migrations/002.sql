CREATE TABLE web_voice_jobs (LIKE mobile_voice_jobs INCLUDING ALL);

INSERT INTO web_voice_schema_migrations (version) VALUES (2);
