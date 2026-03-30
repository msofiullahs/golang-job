package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter implements a token-bucket rate limiter.
// Workers call Wait() before processing each job.
type Limiter struct {
	mu       sync.Mutex
	rate     float64   // tokens per second
	burst    int       // max tokens
	tokens   float64   // current available tokens
	lastTime time.Time // last token refill
}

// New creates a rate limiter that allows `rate` jobs per second
// with a burst capacity.
func New(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastTime: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		l.refill()

		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}

		// Calculate how long until next token
		waitDur := time.Duration(float64(time.Second) / l.rate)
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
			// retry
		}
	}
}

// refill adds tokens based on elapsed time. Must be called with mu held.
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastTime).Seconds()
	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
	l.lastTime = now
}
