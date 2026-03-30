package metrics

import "time"

// Snapshot is a point-in-time view of system metrics.
type Snapshot struct {
	Timestamp     time.Time `json:"timestamp"`
	JobsProcessed int64     `json:"jobs_processed"`
	JobsSucceeded int64     `json:"jobs_succeeded"`
	JobsFailed    int64     `json:"jobs_failed"`
	JobsEnqueued  int64     `json:"jobs_enqueued"`
	JobsRetried   int64     `json:"jobs_retried"`
	JobsDead      int64     `json:"jobs_dead"`
	Throughput    float64   `json:"throughput"` // jobs/sec over last minute
}
