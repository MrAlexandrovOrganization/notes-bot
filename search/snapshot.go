package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// The mirror publishes complete generations under an exclusive flock. Only
// filesystem reconciliation holds the shared lock; slow LLM backfills don't
// prevent a newer vault snapshot from being published.
func acquireSnapshot(ctx context.Context, path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open snapshot lock: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if err == nil {
			var once sync.Once
			return func() { once.Do(func() { _ = file.Close() }) }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock snapshot: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
