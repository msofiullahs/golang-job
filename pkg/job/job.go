package job

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	mrand "math/rand/v2"
	"time"
)

// Priority defines job execution priority. Higher values are processed first.
type Priority int

const (
	PriorityLow      Priority = 1
	PriorityNormal   Priority = 5
	PriorityHigh     Priority = 9
	PriorityCritical Priority = 10
)

// Handler is the interface users implement to define job logic.
type Handler interface {
	Handle(ctx context.Context, payload []byte) error
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, payload []byte) error

func (f HandlerFunc) Handle(ctx context.Context, payload []byte) error {
	return f(ctx, payload)
}

// BackoffFunc computes the delay before retry attempt N.
type BackoffFunc func(attempt int) time.Duration

// DefaultBackoff returns an exponential backoff with jitter.
// 5s, 10s, 20s, 40s ... capped at 30 minutes.
func DefaultBackoff(attempt int) time.Duration {
	base := float64(5 * time.Second)
	max := float64(30 * time.Minute)

	delay := base * math.Pow(2, float64(attempt))
	if delay > max {
		delay = max
	}

	// Add +/- 20% jitter to prevent thundering herd
	jitter := delay * 0.2 * (mrand.Float64()*2 - 1)
	delay += jitter

	return time.Duration(delay)
}

// Options configures a job at enqueue time.
type Options struct {
	Queue      string
	Priority   Priority
	MaxRetries int
	Delay      time.Duration // run after delay from now
	ScheduleAt time.Time     // run at specific time
	Timeout    time.Duration // per-execution timeout
	BackoffFn  BackoffFunc   // nil = DefaultBackoff
	Meta       map[string]string
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		Queue:      "default",
		Priority:   PriorityNormal,
		MaxRetries: 3,
		Timeout:    5 * time.Minute,
	}
}

// Info is the metadata envelope for every job in the system.
type Info struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Queue       string            `json:"queue"`
	Payload     []byte            `json:"payload"`
	Priority    Priority          `json:"priority"`
	Status      Status            `json:"status"`
	MaxRetries  int               `json:"max_retries"`
	Attempts    int               `json:"attempts"`
	LastError   string            `json:"last_error,omitempty"`
	Errors      []string          `json:"errors,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ScheduledAt time.Time         `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// NewID generates a random job ID.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
