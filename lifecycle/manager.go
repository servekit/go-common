package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Manager manages a group of Services with sequential startup and concurrent
// shutdown. Registration methods (Add, AddStarter, AddStopper) must be called
// before Start/Run. Manager is not safe for concurrent registration.
type Manager struct {
	entries     []entry
	startOnce   sync.Once
	startErr    error
	stopOnce    sync.Once
	stopErr     error
	stopTimeout time.Duration
}

// Option configures a Manager at construction time.
type Option func(*Manager)

// entry pairs a name with the registered Service.
type entry struct {
	name string
	svc  Service
}

// NewManager constructs a Manager with the given options applied.
// Default StopTimeout is 10 seconds; pass WithStopTimeout(0) to disable
// (equivalent to &Manager{} zero-value behavior).
func NewManager(opts ...Option) *Manager {
	m := &Manager{stopTimeout: 10 * time.Second}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithStopTimeout sets the total budget for Manager.Stop().
// 0 disables the timeout entirely.
func WithStopTimeout(d time.Duration) Option {
	return func(m *Manager) { m.stopTimeout = d }
}

// Add registers a full Service (start + stop).
func (m *Manager) Add(name string, svc Service) {
	m.entries = append(m.entries, entry{name: name, svc: svc})
}

// AddStarter registers a Starter as a Service with a no-op Stop.
func (m *Manager) AddStarter(name string, s Starter) {
	m.Add(name, starterOnly{s})
}

// AddStopper registers a Stopper as a Service with a no-op Start.
func (m *Manager) AddStopper(name string, s Stopper) {
	m.Add(name, stopperOnly{s})
}

// Start sequentially triggers each Service's Start in registration order.
// If any service fails to start, Start returns the error immediately and
// skips remaining services. Subsequent calls are no-ops and return the same
// result as the first call.
//
// Registered Start methods must be non-blocking: they should launch
// long-running work (listeners, workers, etc.) in goroutines and return
// quickly. A blocking Start would stall the entire startup sequence and
// prevent subsequent services from starting.
func (m *Manager) Start() error {
	m.startOnce.Do(func() {
		for _, e := range m.entries {
			slog.Info("lifecycle: starting", "service", e.name)
			if err := e.svc.Start(); err != nil {
				m.startErr = fmt.Errorf("%s: %w", e.name, err)
				slog.Error("lifecycle: start failed", "service", e.name, "error", err)
				return
			}
			slog.Info("lifecycle: started", "service", e.name)
		}
	})
	return m.startErr
}

// Stop concurrently triggers every Service's Stop via sync.WaitGroup.Go.
// If stopTimeout is non-zero, Stop returns no later than stopTimeout elapses,
// even if some Service.Stop calls are still running (they continue in the
// background; Go cannot kill goroutines). When timeout fires, the returned
// error includes a "lifecycle: stop timeout after Xs" entry alongside any
// per-service errors already collected.
//
// Panics propagate (a panic during shutdown is a bug — let it crash so the
// user notices). Returned errors are aggregated via errors.Join, each wrapped
// with the component name. Subsequent calls return the error captured by the
// first call.
func (m *Manager) Stop() error {
	m.stopOnce.Do(func() {
		var wg sync.WaitGroup
		var mu sync.Mutex
		var errs []error
		for _, e := range m.entries {
			// wg.Go (Go 1.21+) runs defer wg.Done() before re-panicking,
			// so wg.Wait() cannot deadlock if a Stop panics. Do not rewrite
			// as a manual goroutine unless you preserve this guarantee.
			wg.Go(func() {
				if err := e.svc.Stop(); err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", e.name, err))
					mu.Unlock()
				}
			})
		}

		waitDone := make(chan struct{})
		go func() { wg.Wait(); close(waitDone) }()

		var timedOut bool
		if m.stopTimeout > 0 {
			select {
			case <-waitDone:
				// All Stop calls completed within budget.
			case <-time.After(m.stopTimeout):
				timedOut = true
				slog.Warn("lifecycle: stop timeout exceeded, some services may not have finished",
					"timeout", m.stopTimeout)
			}
		} else {
			<-waitDone
		}

		// On timeout, wg.Go goroutines for slow services may still be
		// appending to errs under mu. Snapshot + append + Join must happen
		// under the same lock to avoid racing with those writers.
		mu.Lock()
		if timedOut {
			errs = append(errs, fmt.Errorf("lifecycle: stop timeout after %s", m.stopTimeout))
		}
		m.stopErr = errors.Join(errs...)
		mu.Unlock()
	})
	return m.stopErr
}

// Run starts all services, blocks until ctx is canceled, then stops all.
// A Start failure panics before Run reaches the wait. Returns the Stop result.
func (m *Manager) Run(ctx context.Context) error {
	if err := m.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	return m.Stop()
}
