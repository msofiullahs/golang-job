package metrics

import (
	"fmt"
	"net/http"
)

// PrometheusHandler returns an HTTP handler that exposes metrics
// in Prometheus text exposition format.
func PrometheusHandler(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := c.Snapshot()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprintf(w, "# HELP goqueue_jobs_processed_total Total number of jobs processed\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_processed_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_processed_total %d\n\n", s.JobsProcessed)

		fmt.Fprintf(w, "# HELP goqueue_jobs_succeeded_total Total number of successful jobs\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_succeeded_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_succeeded_total %d\n\n", s.JobsSucceeded)

		fmt.Fprintf(w, "# HELP goqueue_jobs_failed_total Total number of failed jobs\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_failed_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_failed_total %d\n\n", s.JobsFailed)

		fmt.Fprintf(w, "# HELP goqueue_jobs_enqueued_total Total number of enqueued jobs\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_enqueued_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_enqueued_total %d\n\n", s.JobsEnqueued)

		fmt.Fprintf(w, "# HELP goqueue_jobs_retried_total Total number of retried jobs\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_retried_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_retried_total %d\n\n", s.JobsRetried)

		fmt.Fprintf(w, "# HELP goqueue_jobs_dead_total Total number of dead-lettered jobs\n")
		fmt.Fprintf(w, "# TYPE goqueue_jobs_dead_total counter\n")
		fmt.Fprintf(w, "goqueue_jobs_dead_total %d\n\n", s.JobsDead)

		fmt.Fprintf(w, "# HELP goqueue_throughput_jobs_per_second Current job throughput\n")
		fmt.Fprintf(w, "# TYPE goqueue_throughput_jobs_per_second gauge\n")
		fmt.Fprintf(w, "goqueue_throughput_jobs_per_second %.2f\n", s.Throughput)
	}
}
