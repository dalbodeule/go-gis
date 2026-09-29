// Package jobs contains cancellable application work shared by UI and CLI.
package jobs

import (
	"context"
	"sync"
)

// Progress reports a bounded unit of work.
type Progress struct {
	Completed int
	Total     int
	Message   string
}

// Work is a cancellable operation. Implementations should report progress
// through the callback and return ctx.Err() when cancellation is observed.
type Work func(ctx context.Context, report func(Progress)) error

// Runner starts one operation and exposes completion without blocking callers.
type Runner struct{}

// Start starts work in a goroutine. The result channel receives exactly one
// error and is then closed.
func (Runner) Start(ctx context.Context, work Work, report func(Progress)) <-chan error {
	result := make(chan error, 1)
	go func() {
		defer close(result)
		if work == nil {
			result <- nil
			return
		}
		result <- work(ctx, report)
	}()
	return result
}

// Collector is a small progress sink useful for UI adapters and tests.
type Collector struct {
	mu      sync.RWMutex
	latest  Progress
	updates []Progress
}

// Report records one progress event.
func (c *Collector) Report(progress Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = progress
	c.updates = append(c.updates, progress)
}

// Latest returns the most recent event.
func (c *Collector) Latest() Progress {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// Updates returns a snapshot of all recorded events.
func (c *Collector) Updates() []Progress {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Progress(nil), c.updates...)
}
