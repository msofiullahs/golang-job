// Package main demonstrates GoQueue with all features: multiple queues,
// priorities, retries, dashboard, and various job types.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/sofi/goqueue/internal/dashboard"
	"github.com/sofi/goqueue/pkg/broker"
	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/metrics"
	"github.com/sofi/goqueue/pkg/middleware"
	"github.com/sofi/goqueue/pkg/queue"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	backend := queue.NewMemory()
	registry := job.NewRegistry()
	collector := metrics.NewCollector()

	// Register various job types
	registry.Register("email.send", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		var data map[string]string
		json.Unmarshal(payload, &data)
		time.Sleep(time.Duration(50+rand.IntN(200)) * time.Millisecond)
		if rand.IntN(10) == 0 {
			return fmt.Errorf("SMTP timeout for %s", data["to"])
		}
		return nil
	}))

	registry.Register("image.process", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		time.Sleep(time.Duration(200+rand.IntN(800)) * time.Millisecond)
		if rand.IntN(20) == 0 {
			return fmt.Errorf("out of memory during image processing")
		}
		return nil
	}))

	registry.Register("notification.push", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		time.Sleep(time.Duration(30+rand.IntN(100)) * time.Millisecond)
		return nil
	}))

	registry.Register("report.generate", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		time.Sleep(time.Duration(500+rand.IntN(1500)) * time.Millisecond)
		if rand.IntN(3) == 0 {
			return fmt.Errorf("database query timeout")
		}
		return nil
	}))

	registry.Register("webhook.deliver", job.HandlerFunc(func(ctx context.Context, payload []byte) error {
		select {
		case <-time.After(time.Duration(100+rand.IntN(300)) * time.Millisecond):
			if rand.IntN(8) == 0 {
				return fmt.Errorf("HTTP 503 from remote server")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	// Build server
	srv := broker.NewServer(backend, registry, logger)
	srv.ShutdownTimeout = 30 * time.Second
	srv.Use(
		middleware.Recovery(logger),
		middleware.Logging(logger),
		middleware.Timeout(30*time.Second),
	)

	srv.AddQueue(broker.QueueConfig{Name: "critical", Concurrency: 5})
	srv.AddQueue(broker.QueueConfig{Name: "default", Concurrency: 10})
	srv.AddQueue(broker.QueueConfig{Name: "low", Concurrency: 3})

	// Start dashboard
	dash := dashboard.New(backend, collector, srv.Pools, logger)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", metrics.PrometheusHandler(collector))
		logger.Info("dashboard starting", "addr", ":8080")
		if err := dash.Start(":8080"); err != nil && err != http.ErrServerClosed {
			logger.Error("dashboard error", "error", err)
		}
	}()

	// Continuously enqueue jobs after startup
	srv.OnStarted = func() {
		go continuousEnqueue(srv, logger)
	}

	logger.Info("GoQueue Full Demo",
		"dashboard", "http://localhost:8080",
		"prometheus", "http://localhost:8080/metrics",
	)

	if err := srv.Start(context.Background()); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func continuousEnqueue(srv *broker.Server, logger *slog.Logger) {
	ctx := context.Background()
	time.Sleep(time.Second)

	jobTypes := []struct {
		name     string
		queue    string
		priority job.Priority
		payload  func(i int) any
	}{
		{"email.send", "critical", job.PriorityHigh, func(i int) any {
			return map[string]string{"to": fmt.Sprintf("user%d@app.com", i), "subject": "Welcome!"}
		}},
		{"image.process", "default", job.PriorityNormal, func(i int) any {
			return map[string]any{"file": fmt.Sprintf("upload_%d.png", i), "width": 1200}
		}},
		{"notification.push", "default", job.PriorityNormal, func(i int) any {
			return map[string]string{"user_id": fmt.Sprintf("usr_%d", i), "message": "New activity"}
		}},
		{"report.generate", "low", job.PriorityLow, func(i int) any {
			return map[string]string{"report": fmt.Sprintf("monthly_%d", i)}
		}},
		{"webhook.deliver", "default", job.PriorityNormal, func(i int) any {
			return map[string]string{"url": fmt.Sprintf("https://hook.example.com/%d", i)}
		}},
	}

	// Initial burst
	logger.Info("enqueueing initial burst of 50 jobs")
	for i := range 50 {
		jt := jobTypes[i%len(jobTypes)]
		srv.Enqueue(ctx, jt.name, jt.payload(i), job.Options{
			Queue:      jt.queue,
			Priority:   jt.priority,
			MaxRetries: 3,
		})
	}

	// Then steady stream
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	i := 50
	for range ticker.C {
		jt := jobTypes[rand.IntN(len(jobTypes))]
		srv.Enqueue(ctx, jt.name, jt.payload(i), job.Options{
			Queue:      jt.queue,
			Priority:   jt.priority,
			MaxRetries: 3,
		})
		i++
	}
}
