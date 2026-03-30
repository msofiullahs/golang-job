package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sofi/goqueue/pkg/job"
)

func TestMemoryEnqueueDequeue(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	info := &job.Info{
		ID:       "job-1",
		Type:     "test",
		Queue:    "default",
		Priority: job.PriorityNormal,
		Payload:  []byte(`{"hello":"world"}`),
	}

	if err := q.Enqueue(ctx, info); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx, "default")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}

	if got.ID != "job-1" {
		t.Errorf("got ID %q, want %q", got.ID, "job-1")
	}
	if got.Status != job.StatusProcessing {
		t.Errorf("got status %q, want %q", got.Status, job.StatusProcessing)
	}
	if got.Attempts != 1 {
		t.Errorf("got attempts %d, want 1", got.Attempts)
	}
}

func TestMemoryPriorityOrdering(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Enqueue in reverse priority order
	priorities := []job.Priority{job.PriorityLow, job.PriorityNormal, job.PriorityHigh, job.PriorityCritical}
	for i, p := range priorities {
		q.Enqueue(ctx, &job.Info{
			ID:       fmt.Sprintf("job-%d", i),
			Type:     "test",
			Queue:    "default",
			Priority: p,
			Payload:  []byte("{}"),
		})
	}

	// Should dequeue in priority order: Critical, High, Normal, Low
	expected := []job.Priority{job.PriorityCritical, job.PriorityHigh, job.PriorityNormal, job.PriorityLow}
	for i, want := range expected {
		got, err := q.Dequeue(ctx, "default")
		if err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		if got.Priority != want {
			t.Errorf("dequeue %d: got priority %d, want %d", i, got.Priority, want)
		}
	}
}

func TestMemoryAckNack(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	q.Enqueue(ctx, &job.Info{
		ID:         "job-ack",
		Type:       "test",
		Queue:      "default",
		Priority:   job.PriorityNormal,
		MaxRetries: 3,
	})

	info, _ := q.Dequeue(ctx, "default")
	if err := q.Ack(ctx, info); err != nil {
		t.Fatalf("ack: %v", err)
	}

	if info.Status != job.StatusCompleted {
		t.Errorf("got status %q, want completed", info.Status)
	}

	// Test Nack with exhausted retries
	q.Enqueue(ctx, &job.Info{
		ID:         "job-nack",
		Type:       "test",
		Queue:      "default",
		Priority:   job.PriorityNormal,
		MaxRetries: 1,
		Attempts:   1,
	})

	info2, _ := q.Dequeue(ctx, "default")
	q.Nack(ctx, info2, fmt.Errorf("something failed"))

	if info2.Status != job.StatusDead {
		t.Errorf("got status %q, want dead", info2.Status)
	}

	stats, _ := q.Stats(ctx)
	if stats["default"].Dead != 1 {
		t.Errorf("got dead count %d, want 1", stats["default"].Dead)
	}
}

func TestMemoryScheduleAndPromote(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	q.Schedule(ctx, &job.Info{
		ID:          "scheduled-1",
		Type:        "test",
		Queue:       "default",
		Priority:    job.PriorityNormal,
		ScheduledAt: time.Now().Add(-time.Second), // already due
	})

	promoted, err := q.PromoteDue(ctx)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if promoted != 1 {
		t.Errorf("promoted %d, want 1", promoted)
	}

	// Should now be dequeueable
	got, err := q.Dequeue(ctx, "default")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got.ID != "scheduled-1" {
		t.Errorf("got ID %q, want scheduled-1", got.ID)
	}
}

func TestMemoryRetryJob(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	q.Enqueue(ctx, &job.Info{
		ID:         "retry-1",
		Type:       "test",
		Queue:      "default",
		Priority:   job.PriorityNormal,
		MaxRetries: 1,
		Attempts:   1,
	})

	info, _ := q.Dequeue(ctx, "default")
	q.Nack(ctx, info, fmt.Errorf("failed"))

	// Should be dead now
	if info.Status != job.StatusDead {
		t.Fatalf("expected dead, got %s", info.Status)
	}

	// Retry it
	if err := q.RetryJob(ctx, "retry-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	// Should be dequeueable again
	got, err := q.Dequeue(ctx, "default")
	if err != nil {
		t.Fatalf("dequeue after retry: %v", err)
	}
	if got.ID != "retry-1" {
		t.Errorf("got ID %q, want retry-1", got.ID)
	}
}

func TestMemoryConcurrentEnqueueDequeue(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	n := 100

	// Concurrent enqueuers
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q.Enqueue(ctx, &job.Info{
				ID:       fmt.Sprintf("concurrent-%d", i),
				Type:     "test",
				Queue:    "default",
				Priority: job.PriorityNormal,
			})
		}(i)
	}
	wg.Wait()

	// Dequeue all
	dequeued := 0
	for range n {
		_, err := q.Dequeue(ctx, "default")
		if err != nil {
			t.Fatalf("dequeue %d: %v", dequeued, err)
		}
		dequeued++
	}

	if dequeued != n {
		t.Errorf("dequeued %d, want %d", dequeued, n)
	}
}

func TestMemoryDequeueBlocking(t *testing.T) {
	q := NewMemory()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Dequeue from empty queue should block and then return on context cancel
	_, err := q.Dequeue(ctx, "default")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestMemoryStats(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	for i := range 5 {
		q.Enqueue(ctx, &job.Info{
			ID:       fmt.Sprintf("stats-%d", i),
			Type:     "test",
			Queue:    "default",
			Priority: job.PriorityNormal,
		})
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats["default"].Pending != 5 {
		t.Errorf("pending: got %d, want 5", stats["default"].Pending)
	}
}

func TestMemoryDeleteJob(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	q.Enqueue(ctx, &job.Info{
		ID:       "delete-me",
		Type:     "test",
		Queue:    "default",
		Priority: job.PriorityNormal,
	})

	info, _ := q.Dequeue(ctx, "default")
	q.Ack(ctx, info)

	if err := q.DeleteJob(ctx, "delete-me"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := q.GetJob(ctx, "delete-me")
	if err == nil {
		t.Error("expected error getting deleted job")
	}
}
