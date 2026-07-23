package gorx

import (
	"sync"
	"sync/atomic"
)

// TaskRunner schedules tasks with a concurrency limit.
// It uses a semaphore (buffered channel) to cap the number of concurrent goroutines.
type TaskRunner struct {
	limit  chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewTaskRunner creates a TaskRunner with the given concurrency limit.
func NewTaskRunner(limit int) *TaskRunner {
	return &TaskRunner{limit: make(chan struct{}, limit)}
}

// Schedule submits a task. Blocks if the concurrency limit is reached.
// No-op after [TaskRunner.Wait] has been called.
func (t *TaskRunner) Schedule(task func()) {
	if t.closed.Load() {
		return
	}
	t.wg.Go(func() {
		t.limit <- struct{}{}
		defer func() { <-t.limit }()
		task()
	})
}

// Wait marks the runner as closed and waits for all scheduled tasks to finish.
// Safe to call multiple times.
func (t *TaskRunner) Wait() {
	if t.closed.Swap(true) {
		return
	}
	t.wg.Wait()
}
