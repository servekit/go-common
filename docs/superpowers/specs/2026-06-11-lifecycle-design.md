# Lifecycle Manager Design

Date: 2026-06-11

## Goal

Provide a reusable component lifecycle manager in go-common that handles ordered startup, reverse-order shutdown, startup rollback, and graceful shutdown — replacing the ad-hoc cleanup logic currently scattered across services (e.g., the gRPC connection leak in message-service where `MessageService.Close()` doesn't close the GID service).

## Scope

Create the `lifecycle` package in go-common only. No refactoring of existing services in this task.

## Package

`lifecycle` — pure domain utility (not wrapping any external library), follows the same naming category as `gorx`, `captcha`, `ptr`.

## Interfaces

Three composable interfaces:

```go
package lifecycle

import "context"

// Starter needs explicit startup. Start must be non-blocking — it returns
// once the component is ready, spawning any long-running work in goroutines.
type Starter interface {
    Start(ctx context.Context) error
}

// Stopper needs cleanup on shutdown.
type Stopper interface {
    Stop(ctx context.Context) error
}

// Service combines Starter and Stopper for components needing both.
type Service interface {
    Starter
    Stopper
}
```

## Function Adapters

Allow existing `Close()`-style resources (DB pool, Redis client, gRPC connection) to satisfy the interfaces without wrapper structs:

```go
// StopFunc adapts a function to Stopper.
type StopFunc func(ctx context.Context) error

func (f StopFunc) Stop(ctx context.Context) error { return f(ctx) }

// StartFunc adapts a function to Starter.
type StartFunc func(ctx context.Context) error

func (f StartFunc) Start(ctx context.Context) error { return f(ctx) }
```

## Manager

```go
type Manager struct { /* unexported fields */ }

// Add registers a full Service (start + stop).
func (m *Manager) Add(name string, svc Service)

// AddStarter registers a component that only needs startup.
func (m *Manager) AddStarter(name string, s Starter)

// AddStopper registers a component that only needs cleanup.
func (m *Manager) AddStopper(name string, s Stopper)
```

Registration order determines startup order. Shutdown is in reverse registration order.

Each component has a `name` for logging during startup/shutdown.

## Startup

```go
// Start starts all registered components in registration order.
// If component N fails, components 1..N-1 are stopped in reverse order (rollback),
// and the original error is returned.
func (m *Manager) Start(ctx context.Context) error
```

## Shutdown

```go
// Stop stops all registered components in reverse registration order.
// Continues even if some components fail (best-effort cleanup).
// Returns a combined error via errors.Join if any stops failed.
func (m *Manager) Stop(ctx context.Context) error
```

## Run

```go
// Run starts all components, blocks until ctx is canceled, then stops all.
// Returns the Start error if startup fails, or nil on graceful shutdown.
func (m *Manager) Run(ctx context.Context) error
```

## Signal Handling

The `lifecycle` package does NOT handle OS signals. The caller is responsible for creating a context that cancels on signals using `signal.NotifyContext`:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
if err := mgr.Run(ctx); err != nil {
    slog.Error("service exited with error", "error", err)
    os.Exit(1)
}
```

This keeps the package decoupled from signal logic, more testable, and follows modern Go idioms. Graceful shutdown timeout is also controlled by the caller via a deadline context passed to Stop (or by canceling the Run context with a delayed timeout).

## Concurrency

- Manager is safe for concurrent use after Start (Stop can be triggered from a different goroutine than Run).
- Manager is NOT safe for concurrent Add/AddStarter/AddStopper calls — registration must complete before Start/Run is called.

## Error Handling

- Internal errors use `fmt.Errorf("context: %w", err)` for wrapping (this is a foundation library, not business logic — no xerr).
- Stop collects errors via `errors.Join`.
- Start rolls back and returns the original failure error.

## Testing

Unit tests covering:
- Start order is registration order
- Stop order is reverse registration order
- Start rollback on failure stops already-started components
- Stop continues on component failure and returns joined error
- Run blocks until context cancel then stops
- Function adapters (StartFunc, StopFunc) satisfy the interfaces
- Mixed registration (Add + AddStarter + AddStopper) works correctly
- Empty Manager starts and stops without error
- Concurrent Stop from a different goroutine

No external dependencies (no Docker, no Redis) — pure unit tests with fake components.

## File Structure

```
go-common/lifecycle/
├── lifecycle.go    # Starter, Stopper, Service interfaces + StartFunc/StopFunc adapters
├── manager.go      # Manager type and Add/AddStarter/AddStopper/Start/Stop/Run
└── manager_test.go # unit tests
```

## Non-Goals

- This task does NOT refactor grpcx.Server, message-service, or storage-service.
- No dependency injection framework integration.
- No health check orchestration (separate concern).
- No component dependency graph / topological sort — callers register in correct order.
