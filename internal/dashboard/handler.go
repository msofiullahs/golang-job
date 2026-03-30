package dashboard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sofi/goqueue/pkg/job"
	"github.com/sofi/goqueue/pkg/metrics"
	"github.com/sofi/goqueue/pkg/queue"
	"github.com/sofi/goqueue/pkg/worker"
)

// Dashboard is the HTTP server providing the monitoring UI and REST API.
type Dashboard struct {
	backend   queue.Queue
	collector *metrics.Collector
	pools     func() map[string]*worker.Pool
	hub       *SSEHub
	logger    *slog.Logger
	mux       *http.ServeMux
	server    *http.Server
}

// New creates a new dashboard server.
func New(
	backend queue.Queue,
	collector *metrics.Collector,
	pools func() map[string]*worker.Pool,
	logger *slog.Logger,
) *Dashboard {
	if logger == nil {
		logger = slog.Default()
	}

	d := &Dashboard{
		backend:   backend,
		collector: collector,
		pools:     pools,
		hub:       NewSSEHub(logger),
		logger:    logger.With("component", "dashboard"),
		mux:       http.NewServeMux(),
	}

	d.registerRoutes()
	return d
}

func (d *Dashboard) registerRoutes() {
	// API routes
	d.mux.HandleFunc("GET /api/stats", d.handleStats)
	d.mux.HandleFunc("GET /api/queues", d.handleQueues)
	d.mux.HandleFunc("GET /api/queues/{name}/jobs", d.handleQueueJobs)
	d.mux.HandleFunc("GET /api/jobs/{id}", d.handleGetJob)
	d.mux.HandleFunc("POST /api/jobs/{id}/retry", d.handleRetryJob)
	d.mux.HandleFunc("DELETE /api/jobs/{id}", d.handleDeleteJob)

	// SSE endpoint
	d.mux.Handle("GET /api/events", d.hub)

	// Static files (embedded)
	d.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
}

// Start begins the HTTP server and the metrics broadcasting loop.
func (d *Dashboard) Start(addr string) error {
	d.server = &http.Server{
		Addr:    addr,
		Handler: d.corsMiddleware(d.mux),
	}

	// Start broadcasting metrics via SSE
	go d.broadcastLoop()

	d.logger.Info("dashboard started", "addr", addr)
	return d.server.ListenAndServe()
}

// Stop gracefully shuts down the dashboard server.
func (d *Dashboard) Stop(ctx context.Context) error {
	if d.server != nil {
		return d.server.Shutdown(ctx)
	}
	return nil
}

func (d *Dashboard) broadcastLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		snapshot := d.collector.Snapshot()
		stats, _ := d.backend.Stats(context.Background())

		poolStats := make(map[string]int)
		for name, pool := range d.pools() {
			poolStats[name] = pool.ActiveWorkers()
		}

		d.hub.Broadcast(Event{
			Type: "stats",
			Data: map[string]any{
				"metrics": snapshot,
				"queues":  stats,
				"workers": poolStats,
			},
		})
	}
}

func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	snapshot := d.collector.Snapshot()
	stats, err := d.backend.Stats(r.Context())
	if err != nil {
		d.jsonError(w, err, http.StatusInternalServerError)
		return
	}

	poolStats := make(map[string]int)
	for name, pool := range d.pools() {
		poolStats[name] = pool.ActiveWorkers()
	}

	d.jsonResponse(w, map[string]any{
		"metrics": snapshot,
		"queues":  stats,
		"workers": poolStats,
	})
}

func (d *Dashboard) handleQueues(w http.ResponseWriter, r *http.Request) {
	stats, err := d.backend.Stats(r.Context())
	if err != nil {
		d.jsonError(w, err, http.StatusInternalServerError)
		return
	}
	d.jsonResponse(w, stats)
}

func (d *Dashboard) handleQueueJobs(w http.ResponseWriter, r *http.Request) {
	queueName := r.PathValue("name")
	status := r.URL.Query().Get("status")

	filter := queue.ListFilter{
		Queue:  queueName,
		Status: job.Status(status),
		Limit:  50,
	}

	jobs, err := d.backend.ListJobs(r.Context(), filter)
	if err != nil {
		d.jsonError(w, err, http.StatusInternalServerError)
		return
	}
	d.jsonResponse(w, jobs)
}

func (d *Dashboard) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, err := d.backend.GetJob(r.Context(), id)
	if err != nil {
		d.jsonError(w, err, http.StatusNotFound)
		return
	}
	d.jsonResponse(w, j)
}

func (d *Dashboard) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.backend.RetryJob(r.Context(), id); err != nil {
		d.jsonError(w, err, http.StatusBadRequest)
		return
	}
	d.jsonResponse(w, map[string]string{"status": "retried"})
}

func (d *Dashboard) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.backend.DeleteJob(r.Context(), id); err != nil {
		d.jsonError(w, err, http.StatusBadRequest)
		return
	}
	d.jsonResponse(w, map[string]string{"status": "deleted"})
}

func (d *Dashboard) jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (d *Dashboard) jsonError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (d *Dashboard) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
