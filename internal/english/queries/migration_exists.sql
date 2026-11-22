SELECT EXISTS (SELECT 1 FROM english_schema_migrations WHERE version = $1);
