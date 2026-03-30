package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/sofi/goqueue/pkg/queue"
)

// Scheduler periodically checks for delayed/scheduled jobs and promotes
// them to the ready queue when their scheduled time has passed.
type Scheduler struct {
	backend  queue.Queue
	interval time.Duration
	logger   *slog.Logger
}

// New creates a scheduler that checks for due jobs at the given interval.
func New(backend queue.Queue, interval time.Duration, logger *slog.Logger) *Scheduler {
	if interval <= 0 {
		interval = time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		backend:  backend,
		interval: interval,
		logger:   logger.With("component", "scheduler"),
	}
}

// Run starts the scheduler loop. It blocks until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("scheduler started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			promoted, err := s.backend.PromoteDue(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.logger.Error("promote due jobs failed", "error", err)
				continue
			}
			if promoted > 0 {
				s.logger.Info("promoted scheduled jobs", "count", promoted)
			}
		}
	}
}
