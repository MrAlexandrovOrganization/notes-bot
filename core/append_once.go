package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var (
	ErrOperationConflict  = errors.New("append operation already exists with different content or date")
	ErrInvalidOperationID = errors.New("append operation ID must match [A-Za-z0-9_.:-]{1,128}")
	operationIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

const appendMarkerPrefix = "<!-- notes-bot:append:"

// IdempotentNoteStore is optional so existing NoteStore implementations keep
// their contract. Durable callers must fail closed if their store lacks it.
type IdempotentNoteStore interface {
	AppendToNoteOnce(ctx context.Context, date, text, operationID string) error
}

// lockFile uses a stable sidecar rather than the note inode, because atomic
// replacement changes that inode. Every core writer uses the same lock. This
// protects processes sharing a local filesystem; distributed storage requires
// its own locking guarantees.
func lockFile(ctx context.Context, path string) (func(), error) {
	lockPath := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".notes-bot.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open note lock: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close() //nolint:errcheck
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { file.Close() }, nil //nolint:errcheck
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			file.Close() //nolint:errcheck
			return nil, fmt.Errorf("lock note: %w", err)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close() //nolint:errcheck
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open note directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync note directory: %w", err)
	}
	return nil
}

func validateOperationID(operationID string) error {
	if !operationIDPattern.MatchString(operationID) {
		return ErrInvalidOperationID
	}
	return nil
}

func (r *realNoteStore) AppendToNoteOnce(ctx context.Context, date, text, operationID string) error {
	if err := validateDate(date); err != nil {
		return err
	}
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if strings.Contains(text, appendMarkerPrefix) {
		return fmt.Errorf("append text contains a reserved operation marker")
	}
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	// Serialize operation lookup across dates to reject accidental key reuse on
	// another note. Notes are small; scanning avoids a second commit/index file.
	unlockOperations, err := lockFile(ctx, filepath.Join(GetConfig(ctx).DailyNotesDir, "append-operations"))
	if err != nil {
		return err
	}
	defer unlockOperations()
	unlockNote, err := lockFile(ctx, filePath)
	if err != nil {
		return err
	}
	defer unlockNote()

	keyHash := sha256.Sum256([]byte(operationID))
	payloadHash := sha256.Sum256([]byte(date + "\x00" + text))
	keyPrefix := fmt.Sprintf("%s%x:", appendMarkerPrefix, keyHash)
	marker := fmt.Sprintf("%s%x -->", keyPrefix, payloadHash)
	found, err := findAppendOperation(ctx, filePath, keyPrefix, marker)
	if err != nil {
		return err
	}
	if found {
		// A prior rename may have succeeded just before fsync/response failed.
		// Sync again before reporting the retry as successful.
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		err = file.Sync()
		file.Close() //nolint:errcheck
		if err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(filePath))
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := r.createFromTemplate(ctx, filePath, date); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, []byte(text+"\n"+marker+"\n")...)
	return writeFileAtomic(filePath, content, info.Mode().Perm())
}

func findAppendOperation(ctx context.Context, filePath, keyPrefix, expectedMarker string) (bool, error) {
	dailyDir := GetConfig(ctx).DailyNotesDir
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		return false, err
	}
	found := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path, err := resolveVaultPath(dailyDir, entry.Name())
		if err != nil {
			return false, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.Contains(line, keyPrefix) {
				continue
			}
			if path != filePath || line != expectedMarker || found {
				return false, ErrOperationConflict
			}
			found = true
		}
	}
	return found, nil
}
