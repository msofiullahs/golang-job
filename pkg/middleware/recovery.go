package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/sofi/goqueue/pkg/job"
)

// Recovery returns middleware that recovers from panics in job handlers,
// converting them to errors so the worker loop doesn't crash.
func Recovery(logger *slog.Logger) Func {
	return func(next job.Handler) job.Handler {
		return job.HandlerFunc(func(ctx context.Context, payload []byte) (err error) {
			defer func() {
				if r := recover(); r != nil {
					stack := string(debug.Stack())
					logger.Error("job panic recovered",
						"panic", r,
						"stack", stack,
					)
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next.Handle(ctx, payload)
		})
	}
}
