package middleware

import (
	"context"
	"time"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/metrics"
)

// Metrics returns middleware that records job execution metrics.
func Metrics(collector *metrics.Collector, queueName string) Func {
	return func(next job.Handler) job.Handler {
		return job.HandlerFunc(func(ctx context.Context, payload []byte) error {
			start := time.Now()
			collector.RecordProcessed(queueName)

			err := next.Handle(ctx, payload)

			_ = time.Since(start)
			if err != nil {
				collector.RecordFailed(queueName)
			} else {
				collector.RecordSucceeded(queueName)
			}

			return err
		})
	}
}
