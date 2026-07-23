package gorx

import (
	"log/slog"
	"runtime/debug"
)

// OnPanic is called when a goroutine recovers from a panic in [GoSafe] or [RoutineGroup.RunSafe].
// Override this to customize panic handling (e.g., send to Sentry).
// The default behavior logs the panic value and stack trace via slog.
var OnPanic = func(r any, stack []byte) {
	slog.Error("panic recovered", "error", r, "stack", string(stack))
}

// GoUnsafe runs fn in a new goroutine without panic recovery.
func GoUnsafe(fn func()) {
	go fn()
}

// GoSafe runs fn in a new goroutine with panic recovery.
func GoSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				OnPanic(r, debug.Stack())
			}
		}()
		fn()
	}()
}
