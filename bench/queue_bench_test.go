package bench

import (
	"context"
	"fmt"
	"testing"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/queue"
)

func BenchmarkMemoryEnqueue(b *testing.B) {
	q := queue.NewMemory()
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		q.Enqueue(ctx, &job.Info{
			ID:       fmt.Sprintf("job-%d", i),
			Type:     "bench",
			Queue:    "default",
			Priority: job.PriorityNormal,
			Payload:  []byte(`{"key":"value"}`),
		})
	}
}

func BenchmarkMemoryEnqueueDequeue(b *testing.B) {
	q := queue.NewMemory()
	ctx := context.Background()

	// Pre-fill
	for i := range b.N {
		q.Enqueue(ctx, &job.Info{
			ID:       fmt.Sprintf("job-%d", i),
			Type:     "bench",
			Queue:    "default",
			Priority: job.PriorityNormal,
			Payload:  []byte(`{"key":"value"}`),
		})
	}

	b.ResetTimer()
	for range b.N {
		q.Dequeue(ctx, "default")
	}
}

func BenchmarkMemoryPriorityEnqueue(b *testing.B) {
	q := queue.NewMemory()
	ctx := context.Background()
	priorities := []job.Priority{job.PriorityLow, job.PriorityNormal, job.PriorityHigh, job.PriorityCritical}

	b.ResetTimer()
	for i := range b.N {
		q.Enqueue(ctx, &job.Info{
			ID:       fmt.Sprintf("job-%d", i),
			Type:     "bench",
			Queue:    "default",
			Priority: priorities[i%len(priorities)],
			Payload:  []byte(`{"key":"value"}`),
		})
	}
}

func BenchmarkMemoryParallelEnqueue(b *testing.B) {
	q := queue.NewMemory()
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			q.Enqueue(ctx, &job.Info{
				ID:       fmt.Sprintf("job-p-%d", i),
				Type:     "bench",
				Queue:    "default",
				Priority: job.PriorityNormal,
				Payload:  []byte(`{"key":"value"}`),
			})
			i++
		}
	})
}
