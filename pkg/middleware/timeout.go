package middleware

import (
	"context"
	"time"

	"github.com/sofi/goqueue/pkg/job"
)

// Timeout returns middleware that enforces a maximum execution duration per job.
// If the job exceeds the timeout, the context is cancelled.
func Timeout(d time.Duration) Func {
	return func(next job.Handler) job.Handler {
		return job.HandlerFunc(func(ctx context.Context, payload []byte) error {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next.Handle(ctx, payload)
		})
	}
}
