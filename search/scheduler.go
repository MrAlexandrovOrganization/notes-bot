package search

import (
	"context"
	"time"

	"go.uber.org/zap"

	"notes-bot/internal/applog"
)

type indexSyncer interface {
	SyncOnce(context.Context) (SyncStats, error)
}

type Scheduler struct {
	indexer indexSyncer
	cfg     *Config
}

func NewScheduler(indexer *Indexer, cfg *Config) *Scheduler {
	return newScheduler(indexer, cfg)
}

func newScheduler(indexer indexSyncer, cfg *Config) *Scheduler {
	return &Scheduler{indexer: indexer, cfg: cfg}
}

// Run runs the initial sync (best-effort, logs but does not return errors) and
// then loops on a ticker until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	log := applog.With(ctx, logger)

	if _, err := s.indexer.SyncOnce(ctx); err != nil {
		log.Error("initial sync failed", zap.Error(err))
	}

	ticker := time.NewTicker(s.cfg.IndexInterval)
	defer ticker.Stop()
	log.Info("scheduler started", zap.Duration("interval", s.cfg.IndexInterval))

	for {
		select {
		case <-ctx.Done():
			log.Info("scheduler stopped")
			return
		case <-ticker.C:
			if _, err := s.indexer.SyncOnce(ctx); err != nil {
				log.Error("sync failed", zap.Error(err))
			}
		}
	}
}
