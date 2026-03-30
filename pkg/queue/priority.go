package queue

import (
	"container/heap"

	"github.com/sofi/goqueue/pkg/job"
)

// priorityHeap implements heap.Interface for priority-ordered job dequeuing.
// Higher priority jobs come first. Within the same priority, older jobs (FIFO).
type priorityHeap []*job.Info

func (h priorityHeap) Len() int { return len(h) }

func (h priorityHeap) Less(i, j int) bool {
	if h[i].Priority != h[j].Priority {
		return h[i].Priority > h[j].Priority // higher priority first
	}
	return h[i].CreatedAt.Before(h[j].CreatedAt) // older first (FIFO)
}

func (h priorityHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *priorityHeap) Push(x any) {
	*h = append(*h, x.(*job.Info))
}

func (h *priorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[:n-1]
	return item
}

// newPriorityHeap creates an initialized priority heap.
func newPriorityHeap() *priorityHeap {
	h := &priorityHeap{}
	heap.Init(h)
	return h
}
