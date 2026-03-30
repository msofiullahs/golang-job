package job

// Status represents the lifecycle state of a job.
type Status string

const (
	StatusPending    Status = "pending"
	StatusScheduled  Status = "scheduled"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusRetrying   Status = "retrying"
	StatusDead       Status = "dead"
)

// IsTerminal returns true if the job is in a final state.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusDead
}
