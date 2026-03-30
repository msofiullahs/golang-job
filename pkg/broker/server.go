package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/middleware"
	"github.com/sofi/goqueue/pkg/queue"
	"github.com/sofi/goqueue/pkg/scheduler"
	"github.com/sofi/goqueue/pkg/worker"
)

// QueueConfig defines a named queue and its worker pool settings.
type QueueConfig struct {
	Name        string
	Concurrency int
	RateLimit   float64 // jobs/sec, 0 = unlimited
}

// Server is the top-level orchestrator that ties queues, worker pools,
// the scheduler, and the dashboard together.
type Server struct {
	backend    queue.Queue
	registry   *job.Registry
	queues     []QueueConfig
	pools      map[string]*worker.Pool
	scheduler  *scheduler.Scheduler
	middleware []middleware.Func
	logger     *slog.Logger

	mu      sync.Mutex
	running bool

	// OnStarted is called after all pools and scheduler are running.
	OnStarted func()
	// OnStopped is called after graceful shutdown completes.
	OnStopped func()

	// DashboardAddr is the address for the dashboard HTTP server.
	// Set before calling Start(). Empty means no dashboard.
	DashboardAddr string

	// ShutdownTimeout is how long to wait for in-flight jobs.
	ShutdownTimeout time.Duration
}

// NewServer creates a new broker server.
func NewServer(backend queue.Queue, registry *job.Registry, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		backend:         backend,
		registry:        registry,
		pools:           make(map[string]*worker.Pool),
		logger:          logger.With("component", "broker"),
		ShutdownTimeout: 30 * time.Second,
	}
}

// AddQueue registers a named queue with its configuration.
func (s *Server) AddQueue(cfg QueueConfig) {
	s.queues = append(s.queues, cfg)
}

// Use adds middleware that wraps all job handlers.
func (s *Server) Use(mw ...middleware.Func) {
	s.middleware = append(s.middleware, mw...)
}

// Enqueue submits a job with the given type, payload, and options.
func (s *Server) Enqueue(ctx context.Context, typeName string, payload any, opts ...job.Options) (*job.Info, error) {
	opt := job.DefaultOptions()
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Queue == "" {
		opt.Queue = "default"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	info := &job.Info{
		ID:         job.NewID(),
		Type:       typeName,
		Queue:      opt.Queue,
		Payload:    data,
		Priority:   opt.Priority,
		Status:     job.StatusPending,
		MaxRetries: opt.MaxRetries,
		CreatedAt:  time.Now(),
		Meta:       opt.Meta,
	}

	if opt.Delay > 0 {
		info.ScheduledAt = time.Now().Add(opt.Delay)
		info.Status = job.StatusScheduled
		return info, s.backend.Schedule(ctx, info)
	}
	if !opt.ScheduleAt.IsZero() {
		info.ScheduledAt = opt.ScheduleAt
		info.Status = job.StatusScheduled
		return info, s.backend.Schedule(ctx, info)
	}

	return info, s.backend.Enqueue(ctx, info)
}

// Start launches all worker pools, the scheduler, and blocks until
// an OS signal (SIGINT/SIGTERM) triggers graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start scheduler
	s.scheduler = scheduler.New(s.backend, time.Second, s.logger)
	go s.scheduler.Run(ctx)

	// Start worker pools
	for _, cfg := range s.queues {
		opts := worker.PoolOptions{
			Concurrency:     cfg.Concurrency,
			ShutdownTimeout: s.ShutdownTimeout,
		}

		pool := worker.NewPool(cfg.Name, s.backend, s.registry, opts, s.logger)
		s.pools[cfg.Name] = pool
		pool.Start(ctx)
	}

	s.logger.Info("server started",
		"queues", len(s.queues),
		"registered_types", s.registry.Types(),
	)

	if s.OnStarted != nil {
		s.OnStarted()
	}

	// Wait for OS signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		s.logger.Info("received signal, shutting down", "signal", sig)
	case <-ctx.Done():
		s.logger.Info("context cancelled, shutting down")
	}

	return s.shutdown()
}

func (s *Server) shutdown() error {
	s.logger.Info("graceful shutdown started", "timeout", s.ShutdownTimeout)

	// Stop all worker pools (waits for in-flight jobs)
	var wg sync.WaitGroup
	for name, pool := range s.pools {
		wg.Add(1)
		go func(name string, pool *worker.Pool) {
			defer wg.Done()
			pool.Stop()
			s.logger.Info("pool stopped", "queue", name)
		}(name, pool)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("graceful shutdown completed")
	case <-time.After(s.ShutdownTimeout):
		s.logger.Warn("shutdown timeout exceeded, forcing exit")
	}

	if s.OnStopped != nil {
		s.OnStopped()
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// Backend returns the underlying queue backend (for dashboard access).
func (s *Server) Backend() queue.Queue {
	return s.backend
}

// Pools returns the active worker pools (for dashboard stats).
func (s *Server) Pools() map[string]*worker.Pool {
	return s.pools
}
