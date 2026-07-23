// Package lifecycle provides concurrent startup and shutdown for service
// components. Each Service runs in its own goroutine, so blocking starters
// (e.g. http.Server.ListenAndServe) run concurrently. Signal handling is
// left to the caller via signal.NotifyContext; this package orchestrates
// lifecycle only.
package lifecycle

// Starter starts a component. Start may block — for long-running components
// (HTTP server, worker pool) it blocks for the component's lifetime and
// returns an error only on failure. Manager runs each Service in its own
// goroutine, so blocking Starts across services run concurrently.
//
// Start does not receive a context: capture it in the constructor if needed.
type Starter interface {
	Start() error
}

// Stopper stops a component. Stop should release resources and return.
type Stopper interface {
	Stop() error
}

// Service combines Starter and Stopper.
type Service interface {
	Starter
	Stopper
}

// StartFunc adapts a start-only function to Service. Stop is a no-op.
type StartFunc func() error

// StopFunc adapts a stop-only function to Service. Start is a no-op.
type StopFunc func()

// starterOnly wraps a Starter to satisfy Service with a no-op Stop.
type starterOnly struct{ Starter }

// stopperOnly wraps a Stopper to satisfy Service with a no-op Start.
type stopperOnly struct{ Stopper }

// Start runs the underlying function.
func (s StartFunc) Start() error { return s() }

// Stop is a no-op for StartFunc.
func (StartFunc) Stop() error { return nil }

// Start is a no-op for StopFunc.
func (StopFunc) Start() error { return nil }

// Stop runs the underlying function.
func (s StopFunc) Stop() error { s(); return nil }

func (starterOnly) Stop() error { return nil }

func (stopperOnly) Start() error { return nil }
