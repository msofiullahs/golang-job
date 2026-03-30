package middleware

import "github.com/sofi/goqueue/pkg/job"

// Func wraps a job handler, returning a new handler.
// This mirrors the idiomatic Go HTTP middleware pattern.
type Func func(next job.Handler) job.Handler

// Chain composes middleware in order: Chain(a, b, c)(handler)
// executes a -> b -> c -> handler.
func Chain(mws ...Func) Func {
	return func(final job.Handler) job.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			final = mws[i](final)
		}
		return final
	}
}
