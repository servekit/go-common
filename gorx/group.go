package gorx

import (
	"runtime/debug"
	"sync"
)

// RoutineGroup manages a group of goroutines and waits for them to finish.
type RoutineGroup struct {
	wg sync.WaitGroup
}

// WorkGroup runs the same job N times concurrently and waits for completion.
type WorkGroup struct {
	job     func()
	workers int
}

// NewRoutineGroup creates a new RoutineGroup.
func NewRoutineGroup() *RoutineGroup {
	return &RoutineGroup{}
}

// NewWorkGroup creates a WorkGroup that runs job with the given number of workers.
func NewWorkGroup(job func(), workers int) *WorkGroup {
	return &WorkGroup{job: job, workers: workers}
}

// Run runs fn in a new goroutine.
func (g *RoutineGroup) Run(fn func()) {
	g.wg.Go(fn)
}

// RunSafe runs fn in a new goroutine with panic recovery.
func (g *RoutineGroup) RunSafe(fn func()) {
	g.wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				OnPanic(r, debug.Stack())
			}
		}()
		fn()
	})
}

// Wait blocks until all goroutines in the group finish.
func (g *RoutineGroup) Wait() {
	g.wg.Wait()
}

// Run starts all workers and blocks until they all finish.
func (w *WorkGroup) Run() {
	g := NewRoutineGroup()
	for range w.workers {
		g.Run(w.job)
	}
	g.Wait()
}
