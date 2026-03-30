package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/queue"
)

// Pool manages a group of worker goroutines processing jobs from a named queue.
type Pool struct {
	queueName string
	backend   queue.Queue
	registry  *job.Registry
	handler   job.Handler // middleware-wrapped handler (optional override)
	opts      PoolOptions
	logger    *slog.Logger

	mu      sync.Mutex
	workers []*Worker
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	running bool
}

// NewPool creates a worker pool for the given queue.
func NewPool(queueName string, backend queue.Queue, registry *job.Registry, opts PoolOptions, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		queueName: queueName,
		backend:   backend,
		registry:  registry,
		opts:      opts,
		logger:    logger.With("component", "worker_pool", "queue", queueName),
	}
}

// SetHandler sets a middleware-wrapped handler that overrides the registry lookup.
func (p *Pool) SetHandler(h job.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = h
}

// Start launches the configured number of worker goroutines.
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return
	}

	workerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.running = true

	for i := range p.opts.Concurrency {
		w := &Worker{
			id:        i,
			queueName: p.queueName,
			backend:   p.backend,
			registry:  p.registry,
			handler:   p.handler,
			logger:    p.logger,
		}
		p.workers = append(p.workers, w)
		p.wg.Add(1)
		go w.run(workerCtx, p.wg.Done)
	}

	p.logger.Info("pool started", "concurrency", p.opts.Concurrency)
}

// Stop signals all workers to stop and waits for in-flight jobs to complete.
func (p *Pool) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	cancel := p.cancel
	p.mu.Unlock()

	p.logger.Info("stopping pool")
	cancel()
	p.wg.Wait()
	p.logger.Info("pool stopped")
}

// Scale adjusts the number of running workers. If n > current, new workers
// are started. If n < current, excess workers are stopped.
func (p *Pool) Scale(ctx context.Context, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	current := len(p.workers)
	if n == current {
		return
	}

	p.logger.Info("scaling pool", "from", current, "to", n)
	p.opts.Concurrency = n

	// For simplicity, restart the pool with the new concurrency.
	// A production system might do this more granularly.
	if p.running && p.cancel != nil {
		p.cancel()
		p.mu.Unlock()
		p.wg.Wait()
		p.mu.Lock()

		p.workers = nil
		workerCtx, cancel := context.WithCancel(ctx)
		p.cancel = cancel

		for i := range n {
			w := &Worker{
				id:        i,
				queueName: p.queueName,
				backend:   p.backend,
				registry:  p.registry,
				handler:   p.handler,
				logger:    p.logger,
			}
			p.workers = append(p.workers, w)
			p.wg.Add(1)
			go w.run(workerCtx, p.wg.Done)
		}
	}
}

// ActiveWorkers returns the current number of workers.
func (p *Pool) ActiveWorkers() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.workers)
}

// QueueName returns the name of the queue this pool processes.
func (p *Pool) QueueName() string {
	return p.queueName
}
