# Scheduling is installed explicitly; checks use synthetic files, never production.
.PHONY: backup-test
backup-test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_notes_backup.py'

check: backup-test
