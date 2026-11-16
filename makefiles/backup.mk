# Scheduling is installed explicitly; checks use synthetic files, never production.
.PHONY: backup-test
backup-test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_notes_backup.py'

check: backup-test

.PHONY: notes-sync-test
notes-sync-test:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_notes_sync.py'

check: notes-sync-test
