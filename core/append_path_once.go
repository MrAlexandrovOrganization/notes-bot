package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type IdempotentPathNoteStore interface {
	AppendByPathOnce(ctx context.Context, relpath, text, operationID string) error
}

// AppendByPathOnce scopes operation IDs to an existing destination note. Voice
// jobs persist that destination before dispatch and never change it on retry.
func (r *realNoteStore) AppendByPathOnce(ctx context.Context, relpath, text, operationID string) error {
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if strings.Contains(text, appendMarkerPrefix) {
		return fmt.Errorf("append text contains a reserved operation marker")
	}
	filePath, err := resolveVaultPath(GetConfig(ctx).NotesDir, relpath)
	if err != nil {
		return err
	}
	// Fail before creating a sidecar if the selected note no longer exists.
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("append destination is not a regular file")
	}
	unlock, err := lockFile(ctx, filePath)
	if err != nil {
		return err
	}
	defer unlock()
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	keyHash := sha256.Sum256([]byte(operationID))
	payloadHash := sha256.Sum256([]byte(filepath.Clean(relpath) + "\x00" + text))
	keyPrefix := fmt.Sprintf("%s%x:", appendMarkerPrefix, keyHash)
	marker := fmt.Sprintf("%s%x -->", keyPrefix, payloadHash)
	found := false
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.Contains(line, keyPrefix) {
			continue
		}
		if line != marker || found {
			return ErrOperationConflict
		}
		found = true
	}
	if found {
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
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, []byte(text+"\n"+marker+"\n")...)
	return writeFileAtomic(filePath, content, info.Mode().Perm())
}
