package queue

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sofi/goqueue/pkg/job"
)

// Memory is a fully in-memory Queue implementation using a priority heap
// with sync.Cond for efficient blocking dequeue.
type Memory struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queues    map[string]*priorityHeap // ready queues by name
	scheduled []*job.Info              // scheduled jobs (sorted by ScheduledAt)
	jobs      map[string]*job.Info     // all jobs by ID (for lookup/stats)
	stats     map[string]*QueueStats
	closed    bool
}

// NewMemory creates a new in-memory queue backend.
func NewMemory() *Memory {
	m := &Memory{
		queues: make(map[string]*priorityHeap),
		jobs:   make(map[string]*job.Info),
		stats:  make(map[string]*QueueStats),
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *Memory) ensureQueue(name string) *priorityHeap {
	if _, ok := m.queues[name]; !ok {
		m.queues[name] = newPriorityHeap()
	}
	return m.queues[name]
}

func (m *Memory) ensureStats(name string) *QueueStats {
	if _, ok := m.stats[name]; !ok {
		m.stats[name] = &QueueStats{Name: name}
	}
	return m.stats[name]
}

func (m *Memory) Enqueue(ctx context.Context, info *job.Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("queue is closed")
	}

	info.Status = job.StatusPending
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now()
	}

	h := m.ensureQueue(info.Queue)
	heap.Push(h, info)
	m.jobs[info.ID] = info

	s := m.ensureStats(info.Queue)
	s.Pending++

	m.cond.Broadcast() // wake all waiting workers
	return nil
}

func (m *Memory) Dequeue(ctx context.Context, queueName string) (*job.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start a goroutine that broadcasts on context cancellation
	// so blocked workers can wake up and exit.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			m.cond.Broadcast()
		case <-done:
		}
	}()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if m.closed {
			return nil, fmt.Errorf("queue is closed")
		}

		h := m.ensureQueue(queueName)
		if h.Len() > 0 {
			info := heap.Pop(h).(*job.Info)
			now := time.Now()
			info.Status = job.StatusProcessing
			info.StartedAt = &now
			info.Attempts++

			s := m.ensureStats(queueName)
			s.Pending--
			s.Processing++

			return info, nil
		}

		// Block until signaled
		m.cond.Wait()
	}
}

func (m *Memory) Ack(ctx context.Context, info *job.Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	info.Status = job.StatusCompleted
	info.CompletedAt = &now

	s := m.ensureStats(info.Queue)
	s.Processing--
	s.Completed++

	return nil
}

func (m *Memory) Nack(ctx context.Context, info *job.Info, jobErr error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	errMsg := jobErr.Error()
	info.LastError = errMsg
	info.Errors = append(info.Errors, errMsg)

	s := m.ensureStats(info.Queue)
	s.Processing--

	if info.Attempts >= info.MaxRetries {
		info.Status = job.StatusDead
		s.Dead++
	} else {
		info.Status = job.StatusFailed
		s.Failed++
	}

	return nil
}

func (m *Memory) Schedule(ctx context.Context, info *job.Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info.Status = job.StatusScheduled
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now()
	}

	m.scheduled = append(m.scheduled, info)
	m.jobs[info.ID] = info

	s := m.ensureStats(info.Queue)
	s.Scheduled++

	return nil
}

func (m *Memory) PromoteDue(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	promoted := 0
	remaining := m.scheduled[:0]

	for _, info := range m.scheduled {
		if !info.ScheduledAt.After(now) {
			info.Status = job.StatusPending
			h := m.ensureQueue(info.Queue)
			heap.Push(h, info)

			s := m.ensureStats(info.Queue)
			s.Scheduled--
			s.Pending++
			promoted++
		} else {
			remaining = append(remaining, info)
		}
	}
	m.scheduled = remaining

	if promoted > 0 {
		m.cond.Broadcast()
	}

	return promoted, nil
}

func (m *Memory) Stats(ctx context.Context) (map[string]*QueueStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]*QueueStats, len(m.stats))
	for name, s := range m.stats {
		cp := *s
		result[name] = &cp
	}
	return result, nil
}

func (m *Memory) ListJobs(ctx context.Context, filter ListFilter) ([]*job.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []*job.Info
	for _, j := range m.jobs {
		if filter.Queue != "" && j.Queue != filter.Queue {
			continue
		}
		if filter.Status != "" && j.Status != filter.Status {
			continue
		}
		results = append(results, j)
	}

	// Apply offset and limit
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	} else if filter.Offset >= len(results) {
		return nil, nil
	}
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

func (m *Memory) GetJob(ctx context.Context, id string) (*job.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return j, nil
}

func (m *Memory) DeleteJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}

	s := m.ensureStats(j.Queue)
	switch j.Status {
	case job.StatusDead:
		s.Dead--
	case job.StatusFailed:
		s.Failed--
	case job.StatusCompleted:
		s.Completed--
	}

	delete(m.jobs, id)
	return nil
}

func (m *Memory) RetryJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}

	s := m.ensureStats(j.Queue)
	switch j.Status {
	case job.StatusDead:
		s.Dead--
	case job.StatusFailed:
		s.Failed--
	default:
		return fmt.Errorf("job %q is in state %s, cannot retry", id, j.Status)
	}

	j.Status = job.StatusPending
	j.Attempts = 0
	j.LastError = ""
	j.Errors = nil
	j.StartedAt = nil
	j.CompletedAt = nil

	h := m.ensureQueue(j.Queue)
	heap.Push(h, j)
	s.Pending++

	m.cond.Broadcast()
	return nil
}

// Close shuts down the memory queue, unblocking all waiting Dequeue calls.
func (m *Memory) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.cond.Broadcast()
}
