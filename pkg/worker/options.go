package worker

import "time"

// PoolOptions configures a worker pool.
type PoolOptions struct {
	// Concurrency is the number of worker goroutines.
	Concurrency int

	// ShutdownTimeout is the max time to wait for in-flight jobs on shutdown.
	ShutdownTimeout time.Duration

	// PollInterval is how often to check for new jobs when the queue is empty.
	// This is a fallback — the primary mechanism is blocking dequeue.
	PollInterval time.Duration
}

// DefaultPoolOptions returns sensible defaults.
func DefaultPoolOptions() PoolOptions {
	return PoolOptions{
		Concurrency:     5,
		ShutdownTimeout: 30 * time.Second,
		PollInterval:    time.Second,
	}
}
