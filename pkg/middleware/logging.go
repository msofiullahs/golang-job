package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/sofi/goqueue/pkg/job"
)

// Logging returns middleware that logs job execution start, completion, and failure.
func Logging(logger *slog.Logger) Func {
	return func(next job.Handler) job.Handler {
		return job.HandlerFunc(func(ctx context.Context, payload []byte) error {
			start := time.Now()

			logger.Info("job execution started",
				"payload_size", len(payload),
			)

			err := next.Handle(ctx, payload)

			duration := time.Since(start)
			if err != nil {
				logger.Error("job execution failed",
					"duration", duration,
					"error", err,
				)
			} else {
				logger.Info("job execution completed",
					"duration", duration,
				)
			}

			return err
		})
	}
}
