package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/queue"
)

// Worker represents a single goroutine that processes jobs from a queue.
type Worker struct {
	id        int
	queueName string
	backend   queue.Queue
	registry  *job.Registry
	handler   job.Handler // wrapped with middleware
	logger    *slog.Logger
}

// run is the main worker loop. It blocks on Dequeue and processes jobs
// until the context is cancelled.
func (w *Worker) run(ctx context.Context, done func()) {
	defer done()

	w.logger.Info("worker started", "worker_id", w.id, "queue", w.queueName)

	for {
		info, err := w.backend.Dequeue(ctx, w.queueName)
		if err != nil {
			if ctx.Err() != nil {
				w.logger.Info("worker stopping", "worker_id", w.id, "queue", w.queueName)
				return
			}
			w.logger.Error("dequeue error", "worker_id", w.id, "error", err)
			time.Sleep(time.Second) // brief pause before retry
			continue
		}

		w.process(ctx, info)
	}
}

func (w *Worker) process(ctx context.Context, info *job.Info) {
	logger := w.logger.With(
		"job_id", info.ID,
		"job_type", info.Type,
		"queue", info.Queue,
		"attempt", info.Attempts,
	)

	logger.Info("processing job")
	start := time.Now()

	// Look up the handler from the registry
	handler, err := w.registry.Get(info.Type)
	if err != nil {
		logger.Error("no handler for job type", "error", err)
		_ = w.backend.Nack(ctx, info, fmt.Errorf("no handler: %w", err))
		return
	}

	// Wrap with middleware if set
	if w.handler != nil {
		handler = w.handler
	}

	// Execute the job
	jobErr := handler.Handle(ctx, info.Payload)

	duration := time.Since(start)

	if jobErr != nil {
		logger.Error("job failed",
			"error", jobErr,
			"duration", duration,
		)
		if nackErr := w.backend.Nack(ctx, info, jobErr); nackErr != nil {
			logger.Error("nack failed", "error", nackErr)
		}

		// If job can be retried, schedule it with backoff
		if info.Attempts < info.MaxRetries {
			backoff := job.DefaultBackoff(info.Attempts)
			info.Status = job.StatusRetrying
			info.ScheduledAt = time.Now().Add(backoff)
			if schedErr := w.backend.Schedule(ctx, info); schedErr != nil {
				logger.Error("retry schedule failed", "error", schedErr)
			} else {
				logger.Info("job scheduled for retry",
					"next_attempt_in", backoff,
				)
			}
		}
		return
	}

	logger.Info("job completed", "duration", duration)
	if ackErr := w.backend.Ack(ctx, info); ackErr != nil {
		logger.Error("ack failed", "error", ackErr)
	}
}
