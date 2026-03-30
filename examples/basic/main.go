// Package main demonstrates the simplest possible GoQueue usage.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/sofi/goqueue/pkg/broker"
	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/middleware"
	"github.com/sofi/goqueue/pkg/queue"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// 1. Create an in-memory backend
	backend := queue.NewMemory()

	// 2. Register job handlers
	registry := job.NewRegistry()
	registry.Register("greeting", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		fmt.Printf("Hello! Processing: %s\n", payload)
		time.Sleep(100 * time.Millisecond)
		return nil
	}))

	// 3. Create and configure the server
	srv := broker.NewServer(backend, registry, logger)
	srv.Use(middleware.Recovery(logger), middleware.Logging(logger))
	srv.AddQueue(broker.QueueConfig{Name: "default", Concurrency: 3})

	// 4. Enqueue some jobs after start
	srv.OnStarted = func() {
		go func() {
			ctx := context.Background()
			for i := range 10 {
				srv.Enqueue(ctx, "greeting", fmt.Sprintf("Job #%d", i), job.Options{
					Queue:      "default",
					Priority:   job.PriorityNormal,
					MaxRetries: 2,
				})
			}
			fmt.Println("10 jobs enqueued!")
		}()
	}

	// 5. Start blocks until SIGINT/SIGTERM
	fmt.Println("Starting GoQueue... Press Ctrl+C to stop.")
	if err := srv.Start(context.Background()); err != nil {
		logger.Error("server error", "error", err)
	}
}
