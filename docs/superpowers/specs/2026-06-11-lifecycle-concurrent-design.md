# Lifecycle Manager v2: Sequential Startup, Concurrent Shutdown

Date: 2026-06-11

Replaces the synchronous model described in `2026-06-11-lifecycle-design.md`.

## Background

The current `lifecycle` package uses a concurrent startup model: `Start`
launches each Service in its own goroutine, and errors propagate via panics.
While this allows blocking starters, it introduces problems:

- **No startup ordering.** Components that depend on each other cannot express
  "A must start before B."
- **Error attribution is poor.** A panic from a background goroutine gives no
  structured error to the caller — `Start()` always returns nil.
- **No fail-fast.** If service A fails immediately but service B is slow to
  start, both run concurrently with unpredictable state.

Since all well-designed services have non-blocking `Start()` methods (they
launch listeners/workers in goroutines and return quickly), the original
justification for concurrent startup (supporting blocking starters) no longer
applies in practice.

## Goal

Change `Manager.Start()` to sequential execution: start each service in
registration order, return the first error immediately, and skip remaining
services. Keep `Stop()` concurrent (all services stop in parallel).

## Interfaces

Unchanged from the concurrent model — `Starter`, `Stopper`, `Service` with
no `context.Context` parameter:

```go
package lifecycle

// Starter starts a component. Start must be non-blocking: it should return
// quickly after setup, spawning any long-running work (listeners, workers)
// in goroutines. Manager starts services sequentially, so a blocking Start
// would stall the entire startup sequence.
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
```

## Function Adapters

Both adapters satisfy the full `Service` interface by filling the missing
side with a no-op. This lets `Manager.entries` hold a uniform `[]Service`
instead of separately tracking starter and stopper pointers.

```go
// StartFunc adapts a start-only function to Service. Stop is a no-op.
type StartFunc func() error

func (s StartFunc) Start() error { return s() }
func (s StartFunc) Stop() error  { return nil }

// StopFunc adapts a stop-only function to Service. Start is a no-op.
type StopFunc func()

func (s StopFunc) Start() error { return nil }
func (s StopFunc) Stop() error  { s(); return nil }
```

## Wrappers for AddStarter / AddStopper

Convenience methods accept a single-interface component and wrap it into a
Service. The wrapper embeds the original interface and provides a no-op
implementation for the missing side via method promotion.

```go
// starterOnly wraps a Starter to satisfy Service with a no-op Stop.
type starterOnly struct{ Starter }

func (starterOnly) Stop() error { return nil }

// stopperOnly wraps a Stopper to satisfy Service with a no-op Start.
type stopperOnly struct{ Stopper }

func (stopperOnly) Start() error { return nil }
```

## Manager

```go
type Manager struct {
    entries   []entry
    startOnce sync.Once
    startErr  error
    stopOnce  sync.Once
    stopErr   error
}

type entry struct {
    name string
    svc  Service
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
```

## Start

```go
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
```

Notes:
- Sequential execution in registration order — deterministic, predictable.
- First error stops the startup; caller gets a clear `"component: error"` message.
- `slog.Info` logs before and after each service start; `slog.Error` on failure.
- `sync.Once` ensures idempotency — repeated calls return the same result.

## Stop

```go
// Stop concurrently triggers every Service's Stop via sync.WaitGroup.Go.
// Panics propagate (a panic during shutdown is a bug — let it crash so the
// user notices). Returned errors are aggregated via errors.Join, each
// wrapped with the component name. Subsequent calls return the captured
// error from the first call (sync.Once).
func (m *Manager) Stop() error {
    m.stopOnce.Do(func() {
        var wg sync.WaitGroup
        var mu sync.Mutex
        var errs []error
        for _, e := range m.entries {
            wg.Go(func() {
                if err := e.svc.Stop(); err != nil {
                    mu.Lock()
                    errs = append(errs, fmt.Errorf("%s: %w", e.name, err))
                    mu.Unlock()
                }
            })
        }
        wg.Wait()
        m.stopErr = errors.Join(errs...)
    })
    return m.stopErr
}
```

Notes:
- `sync.WaitGroup.Go` (Go 1.20+) is equivalent to
  `wg.Add(1); go func(){ defer wg.Done(); f() }()`. A panic in `f` runs
  `defer wg.Done()` first, so `wg.Wait()` never deadlocks; the panic then
  propagates up through `Stop`.
- `m.stopErr` is assigned inside `Once.Do`. `sync.Once` provides the memory
  barrier, so the read after `Do` returns is safe across goroutines — every
  caller of `Stop` observes the same aggregated error.

## Run

```go
// Run starts all services, blocks until ctx is canceled, then stops all.
// Returns the Start error if startup fails, or the Stop result.
func (m *Manager) Run(ctx context.Context) error {
    if err := m.Start(); err != nil {
        return err
    }
    <-ctx.Done()
    return m.Stop()
}
```

`m.Start()` now returns a real error on failure, so the `if err != nil` branch
can actually fire. Callers should check the return value.

## Concurrency Semantics

- **Registration** (`Add` / `AddStarter` / `AddStopper`): not safe for
  concurrent use. Must complete before `Start` / `Run`.
- **Start**: idempotent via `sync.Once`. Concurrent or repeated calls after
  the first are no-ops.
- **Stop**: idempotent via `sync.Once`. Concurrent or repeated calls return
  the same `m.stopErr` captured by the first call.
- **Start vs Stop ordering**: not enforced. Callers pair them via `Run`, or
  call `Start` then later `Stop` explicitly.

## Trade-offs

What we changed from the concurrent model:
- **Startup ordering.** Services now start in registration order — deterministic
  and predictable. Dependencies are handled by registering in the correct order.
- **Error as value.** `Start()` returns a real error on failure, wrapped with
  the component name. Callers can inspect and handle it.
- **Fail-fast.** First startup failure stops the sequence immediately.

What we keep from the concurrent model:
- **Concurrent Stop.** All services stop in parallel — shutdown should be fast.
- **No rollback.** If service N fails to start, services 1..N-1 are already
  running. The process should exit (via signalx.Run's panic or caller's choice);
  OS cleans up resources.

What we require:
- **Non-blocking Start.** Each registered `Start()` must return quickly,
  launching long-running work in goroutines. This is the natural pattern for
  gRPC servers, HTTP listeners, and worker pools.

## Error Handling

- `Start` returns the first error encountered, wrapped as `"component: error"`.
  No panic — the caller decides how to handle startup failure.
- `panic` during Stop propagates (no recover). A panic in cleanup is a bug
  that should be visible, not silently logged.
- `Stop` returns `errors.Join` of all stopper-returned errors, each wrapped
  with its component name via `fmt.Errorf("%s: %w", name, err)`.
- This is a foundation library — no `xerr`.

## Dependencies

The `lifecycle` package no longer imports `gorx`. All goroutine launching
uses language primitives (`go func()`, `sync.WaitGroup.Go`).

## Testing

**Delete** (semantics no longer apply):
- `TestManager_Start_ConcurrentTrigger` — Start is sequential, not concurrent.
- `TestManager_Start_PanicsOnError` — Start returns errors, no panic.

**Keep**:
- `TestManager_Stop_ConcurrentTrigger` — Stop is still concurrent.
- `TestManager_Stop_JoinsErrors` — unchanged.
- `TestManager_Run_BlocksUntilContextCancelThenStops` — unchanged.
- `TestStartFunc_*` / `TestStopFunc_*` — unchanged.
- `TestManager_AddStarter_WrapsAsService` / `TestManager_AddStopper_WrapsAsService` — unchanged.
- `TestManager_Start_OnlyOnce` / `TestManager_Stop_OnlyOnce` — unchanged.

**Add**:
- `TestManager_Start_SequentialTrigger` — all starters invoked in registration order.
- `TestManager_Start_SequentialOrder` — verifies exact registration order.
- `TestManager_Start_FailStopsOnFirstError` — first error stops startup; later services never start.

No external dependencies (no Docker, no Redis) — pure unit tests with fake
components. Coverage target 85% per CLAUDE.md.

## File Structure

Unchanged from v1:
```
go-common/lifecycle/
├── lifecycle.go    # Starter, Stopper, Service + StartFunc/StopFunc + wrappers
├── manager.go      # Manager + Add/AddStarter/AddStopper/Start/Stop/Run
├── lifecycle_test.go
└── manager_test.go
```

## Non-Goals

- ~~No graceful-shutdown timeout at the Manager level.~~ **Superseded by
  `2026-06-12-shutdown-reliability-design.md`** — Manager now provides
  `NewManager` + `WithStopTimeout` (default 10s) as a total-budget backstop.
  Per-service `context.WithTimeout` inside individual `Stop()` methods remains
  the recommended first line of defense.
- No dependency ordering. Register dependent components separately, or
  initialize them before `Manager.Start`.
- No signal handling. The caller uses `signal.NotifyContext` and passes the
  resulting ctx to `Run`.
- No refactor of existing call sites — separate task.
