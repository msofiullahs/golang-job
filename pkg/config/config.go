package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the GoQueue server.
type Config struct {
	// Redis connection
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Dashboard
	DashboardAddr string

	// Server
	ShutdownTimeout time.Duration

	// Queues
	Queues []QueueConfig

	// UseRedis controls whether to use Redis or in-memory backend.
	UseRedis bool
}

// QueueConfig defines a named queue.
type QueueConfig struct {
	Name        string
	Concurrency int
	RateLimit   float64
}

// Default returns a default configuration.
func Default() Config {
	return Config{
		RedisAddr:     envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       envIntOr("REDIS_DB", 0),
		DashboardAddr: envOr("DASHBOARD_ADDR", ":8080"),
		ShutdownTimeout: 30 * time.Second,
		UseRedis:      os.Getenv("USE_REDIS") == "true",
		Queues: []QueueConfig{
			{Name: "critical", Concurrency: 5},
			{Name: "default", Concurrency: 10},
			{Name: "low", Concurrency: 3},
		},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
