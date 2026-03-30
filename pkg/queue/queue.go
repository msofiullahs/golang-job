package queue

import (
	"context"

	"github.com/sofi/goqueue/pkg/job"
)

// Queue is the storage backend interface.
// Implementations must be safe for concurrent use.
type Queue interface {
	// Enqueue adds a job to the ready queue, respecting priority ordering.
	Enqueue(ctx context.Context, info *job.Info) error

	// Dequeue blocks until a job is available or ctx is cancelled.
	// Returns the highest-priority job from the named queue.
	Dequeue(ctx context.Context, queueName string) (*job.Info, error)

	// Ack marks a job as successfully completed.
	Ack(ctx context.Context, info *job.Info) error

	// Nack marks a job as failed. The caller decides retry vs dead-letter.
	Nack(ctx context.Context, info *job.Info, jobErr error) error

	// Schedule places a job into the scheduled set for future execution.
	Schedule(ctx context.Context, info *job.Info) error

	// PromoteDue moves jobs whose ScheduledAt <= now into the ready queue.
	PromoteDue(ctx context.Context) (int, error)

	// Stats returns current counts per queue.
	Stats(ctx context.Context) (map[string]*QueueStats, error)

	// ListJobs returns jobs matching the filter.
	ListJobs(ctx context.Context, filter ListFilter) ([]*job.Info, error)

	// GetJob returns a single job by ID.
	GetJob(ctx context.Context, id string) (*job.Info, error)

	// DeleteJob removes a job.
	DeleteJob(ctx context.Context, id string) error

	// RetryJob moves a dead/failed job back to pending.
	RetryJob(ctx context.Context, id string) error
}

// QueueStats holds counters for a single named queue.
type QueueStats struct {
	Name       string `json:"name"`
	Pending    int64  `json:"pending"`
	Scheduled  int64  `json:"scheduled"`
	Processing int64  `json:"processing"`
	Completed  int64  `json:"completed"`
	Failed     int64  `json:"failed"`
	Dead       int64  `json:"dead"`
}

// ListFilter controls job listing.
type ListFilter struct {
	Queue  string
	Status job.Status
	Limit  int
	Offset int
}
