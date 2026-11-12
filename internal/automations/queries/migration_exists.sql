SELECT EXISTS(SELECT 1 FROM automation_migrations WHERE name=$1)
