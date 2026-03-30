package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector aggregates internal metrics for the job queue system.
type Collector struct {
	mu sync.RWMutex

	// Counters
	jobsProcessed atomic.Int64
	jobsSucceeded atomic.Int64
	jobsFailed    atomic.Int64
	jobsEnqueued  atomic.Int64
	jobsRetried   atomic.Int64
	jobsDead      atomic.Int64

	// Per-queue counters
	queueProcessed sync.Map // map[string]*atomic.Int64
	queueFailed    sync.Map

	// Throughput tracking
	throughputWindow []timestampedCount
	windowSize       time.Duration
}

type timestampedCount struct {
	time  time.Time
	count int64
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		windowSize: time.Minute,
	}
}

// RecordProcessed increments the processed counter for a queue.
func (c *Collector) RecordProcessed(queue string) {
	c.jobsProcessed.Add(1)
	c.getOrCreateCounter(&c.queueProcessed, queue).Add(1)

	c.mu.Lock()
	c.throughputWindow = append(c.throughputWindow, timestampedCount{
		time:  time.Now(),
		count: 1,
	})
	c.mu.Unlock()
}

// RecordSucceeded increments the succeeded counter.
func (c *Collector) RecordSucceeded(queue string) {
	c.jobsSucceeded.Add(1)
}

// RecordFailed increments the failed counter for a queue.
func (c *Collector) RecordFailed(queue string) {
	c.jobsFailed.Add(1)
	c.getOrCreateCounter(&c.queueFailed, queue).Add(1)
}

// RecordEnqueued increments the enqueued counter.
func (c *Collector) RecordEnqueued() {
	c.jobsEnqueued.Add(1)
}

// RecordRetried increments the retried counter.
func (c *Collector) RecordRetried() {
	c.jobsRetried.Add(1)
}

// RecordDead increments the dead letter counter.
func (c *Collector) RecordDead() {
	c.jobsDead.Add(1)
}

// Snapshot returns a point-in-time snapshot of all metrics.
func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	// Clean old entries from throughput window
	cutoff := time.Now().Add(-c.windowSize)
	clean := c.throughputWindow[:0]
	for _, tc := range c.throughputWindow {
		if tc.time.After(cutoff) {
			clean = append(clean, tc)
		}
	}
	c.throughputWindow = clean
	throughput := float64(len(clean)) / c.windowSize.Seconds()
	c.mu.Unlock()

	return Snapshot{
		Timestamp:     time.Now(),
		JobsProcessed: c.jobsProcessed.Load(),
		JobsSucceeded: c.jobsSucceeded.Load(),
		JobsFailed:    c.jobsFailed.Load(),
		JobsEnqueued:  c.jobsEnqueued.Load(),
		JobsRetried:   c.jobsRetried.Load(),
		JobsDead:      c.jobsDead.Load(),
		Throughput:    throughput,
	}
}

func (c *Collector) getOrCreateCounter(m *sync.Map, key string) *atomic.Int64 {
	if v, ok := m.Load(key); ok {
		return v.(*atomic.Int64)
	}
	counter := &atomic.Int64{}
	actual, _ := m.LoadOrStore(key, counter)
	return actual.(*atomic.Int64)
}
