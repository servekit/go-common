// Package signalx bridges OS signals to service lifecycle shutdown.
//
// Run starts a Service, blocks until a configured signal arrives, then stops
// the Service. Any type implementing the Service interface (Start + Stop) can
// be managed by signalx.
package signalx

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Service is the lifecycle interface that signalx manages.
type Service interface {
	Start() error
	Stop() error
}

// DefaultSignals are the conventional graceful-shutdown signals on Unix.
var DefaultSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// Run starts s, blocks until a signal in sigs arrives (default SIGINT/SIGTERM),
// then stops s and returns the stop result. Start failures panic.
//
// Run does not log, os.Exit, or handle a second signal — caller decides policy.
func Run(s Service, sigs ...os.Signal) error {
	if len(sigs) == 0 {
		sigs = DefaultSignals
	}
	if err := s.Start(); err != nil {
		panic(fmt.Sprintf("signalx: start failed: %v", err))
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	defer signal.Stop(ch)
	<-ch
	return s.Stop()
}

// RunWithForceQuit is like Run but, if a second signal arrives while the
// service is stopping, immediately sends SIGKILL to the current process.
//
// Use when callers want a second Ctrl+C (or external kill during graceful
// shutdown) to forcefully terminate without waiting for Stop to complete.
// defer statements do not run after SIGKILL — same as an external kill -9.
//
// Run (without force-quit) remains available for callers who prefer the
// original "caller decides policy" behavior.
func RunWithForceQuit(s Service, sigs ...os.Signal) error {
	if len(sigs) == 0 {
		sigs = DefaultSignals
	}
	if err := s.Start(); err != nil {
		panic(fmt.Sprintf("signalx: start failed: %v", err))
	}

	// Buffer 2 so a rapid double-tap (user pressing Ctrl+C twice before
	// this select begins) does not drop the second signal delivery.
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, sigs...)
	defer signal.Stop(ch)

	<-ch // first signal → trigger Stop

	// Buffer 1: Stop writes exactly once; select consumes once or never
	// (the SIGKILL path abandons the goroutine).
	stopDone := make(chan error, 1)
	go func() { stopDone <- s.Stop() }()

	select {
	case err := <-stopDone:
		return err
	case <-ch: // second signal during Stop → SIGKILL self
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGKILL) //nolint:errcheck // SIGKILL to self terminates the process; any error is moot
		return nil                                          // unreachable; SIGKILL cannot be caught
	}
}
