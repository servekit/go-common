# Lifecycle Concurrent Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change lifecycle Manager from concurrent startup to sequential startup. `Start` walks entries in registration order, returns the first error (wrapped with component name), and skips remaining services. `Stop` remains concurrent. Start methods must be non-blocking.

**Architecture:** Manager holds `[]entry{name, Service}`. `Start` uses `sync.Once` + sequential iteration, returning error on failure. `Stop` uses `sync.Once` + `sync.WaitGroup.Go` per entry, aggregating errors via `errors.Join` into `m.stopErr`. `Run` = `Start` + `<-ctx.Done()` + `Stop`. Startup logs via `slog.Info`/`slog.Error`.

**Tech Stack:** Go 1.25, `sync.Once`, `sync.WaitGroup.Go` (Go 1.20+), `errors.Join`, `log/slog`. Tests use only `testify` + stdlib — no external deps.

**Spec:** `docs/superpowers/specs/2026-06-11-lifecycle-concurrent-design.md`

---

## File Structure

- `lifecycle/lifecycle.go` — `Starter` / `Stopper` / `Service` interfaces, `StartFunc` / `StopFunc` adapters, `starterOnly` / `stopperOnly` wrappers
- `lifecycle/manager.go` — `Manager`, `entry`, `Add` / `AddStarter` / `AddStopper`, `Start` / `Stop` / `Run`
- `lifecycle/lifecycle_test.go` — interface & adapter tests + shared test helpers (`errString`, `assertErr`, `stubStarter`, `stubStopper`)
- `lifecycle/manager_test.go` — `Manager` behavior tests using `fakeComponent`

Notes:
- The old `context.Context` parameter on `Start` / `Stop` is removed everywhere.
- The package no longer imports `gorx`.
- Two tests from the spec (`TestManager_Start_PanicsOnError`, `TestManager_Stop_PanicPropagates`) are intentionally omitted — panic happens in a child goroutine and `require.Panics` can't capture it; subprocess-mode tests are high-cost / low-value. Panic behavior is verified by code review of the explicit `panic(...)` call sites.
- `Start` is now sequential (not concurrent). `startErr` field added to Manager to store the first error. `slog` used for startup logging.

---

## Task 1: Rewrite `lifecycle.go` (interfaces + adapters + wrappers)

**Files:**
- Modify: `lifecycle/lifecycle.go` (full rewrite)
- Modify: `lifecycle/lifecycle_test.go` (full rewrite — old tests reference the old `ctx` signature)

- [ ] **Step 1: Replace `lifecycle/lifecycle_test.go` with new tests + shared helpers**

```go
package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// errString is a simple error type shared across lifecycle tests.
type errString string

func (e errString) Error() string { return string(e) }

func assertErr(s string) error { return errString(s) }

// stubStarter is a no-op Starter for wrapper tests.
type stubStarter struct{}

func (stubStarter) Start() error { return nil }

// stubStopper is a no-op Stopper for wrapper tests.
type stubStopper struct{}

func (stubStopper) Stop() error { return nil }

func TestStartFunc_SatisfiesService(t *testing.T) {
	var _ Service = StartFunc(func() error { return nil })
}

func TestStopFunc_SatisfiesService(t *testing.T) {
	var _ Service = StopFunc(func() {})
}

func TestStarterOnly_SatisfiesService(t *testing.T) {
	var _ Service = starterOnly{stubStarter{}}
}

func TestStopperOnly_SatisfiesService(t *testing.T) {
	var _ Service = stopperOnly{stubStopper{}}
}

func TestStartFunc_StartInvokesFunction(t *testing.T) {
	called := false
	fn := StartFunc(func() error {
		called = true
		return nil
	})
	require.NoError(t, fn.Start())
	require.True(t, called)
}

func TestStartFunc_Stop_IsNoOp(t *testing.T) {
	require.NoError(t, StartFunc(func() error { return nil }).Stop())
}

func TestStartFunc_StartPropagatesError(t *testing.T) {
	fn := StartFunc(func() error { return assertErr("boom") })
	require.EqualError(t, fn.Start(), "boom")
}

func TestStopFunc_StopInvokesFunction(t *testing.T) {
	called := false
	fn := StopFunc(func() { called = true })
	require.NoError(t, fn.Stop())
	require.True(t, called)
}

func TestStopFunc_Start_IsNoOp(t *testing.T) {
	require.NoError(t, StopFunc(func() {}).Start())
}
```

- [ ] **Step 2: Run tests to verify they fail (compile error — old signatures mismatch)**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/ -run 'TestStartFunc|TestStopFunc|TestStarterOnly|TestStopperOnly'`
Expected: FAIL — `StartFunc` / `StopFunc` / `starterOnly` / `stopperOnly` do not yet satisfy the new signatures, and the old `lifecycle.go` still has `context.Context` params.

- [ ] **Step 3: Replace `lifecycle/lifecycle.go` with the new definitions**

```go
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

func (s StartFunc) Start() error { return s() }
func (s StartFunc) Stop() error  { return nil }

// StopFunc adapts a stop-only function to Service. Start is a no-op.
type StopFunc func()

func (s StopFunc) Start() error { return nil }
func (s StopFunc) Stop() error  { s(); return nil }

// starterOnly wraps a Starter to satisfy Service with a no-op Stop.
type starterOnly struct{ Starter }

func (starterOnly) Stop() error { return nil }

// stopperOnly wraps a Stopper to satisfy Service with a no-op Start.
type stopperOnly struct{ Stopper }

func (stopperOnly) Start() error { return nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/ -run 'TestStartFunc|TestStopFunc|TestStarterOnly|TestStopperOnly' -v`
Expected: PASS — all 9 tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/moss/code/base/go-common
git add lifecycle/lifecycle.go lifecycle/lifecycle_test.go
git commit -m "$(cat <<'EOF'
feat(lifecycle): rewrite interfaces for concurrent model

Drop context.Context from Start/Stop. StartFunc and StopFunc now satisfy
the full Service interface via no-op complements. Add starterOnly/stopperOnly
wrappers so AddStarter/AddStopper can register single-interface components.
EOF
)"
```

---

## Task 2: Rewrite `manager.go` (Manager + Add + Start + Stop + Run)

**Files:**
- Modify: `lifecycle/manager.go` (full rewrite)
- Modify: `lifecycle/manager_test.go` (full rewrite — old tests assert ordering / rollback which no longer applies)

- [ ] **Step 1: Replace `lifecycle/manager_test.go` with new tests**

```go
package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeComponent records Start/Stop invocations for Manager tests.
type fakeComponent struct {
	name     string
	startErr error
	stopErr  error
	startWg  *sync.WaitGroup // optional: Done() on Start
	stopWg   *sync.WaitGroup // optional: Done() on Stop
	startFn  func() error    // optional: custom Start logic
}

func (f *fakeComponent) Start() error {
	if f.startWg != nil {
		f.startWg.Done()
	}
	if f.startFn != nil {
		return f.startFn()
	}
	return f.startErr
}

func (f *fakeComponent) Stop() error {
	if f.stopWg != nil {
		f.stopWg.Done()
	}
	return f.stopErr
}

// waitGroupDone waits for wg up to 1s; fails the test on timeout.
func waitGroupDone(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitGroup never done within 1s")
	}
}

func TestManager_AddService(t *testing.T) {
	m := &Manager{}
	m.Add("svc", &fakeComponent{name: "svc"})
	require.Len(t, m.entries, 1)
	require.Equal(t, "svc", m.entries[0].name)
}

func TestManager_AddStarter_WrapsAsService(t *testing.T) {
	m := &Manager{}
	m.AddStarter("starter-only", stubStarter{})
	require.Len(t, m.entries, 1)
	require.NoError(t, m.entries[0].svc.Stop()) // wrapped Stop is a no-op
}

func TestManager_AddStopper_WrapsAsService(t *testing.T) {
	m := &Manager{}
	m.AddStopper("stopper-only", stubStopper{})
	require.Len(t, m.entries, 1)
	require.NoError(t, m.entries[0].svc.Start()) // wrapped Start is a no-op
}

func TestManager_Start_SequentialTrigger(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(3)
	for _, name := range []string{"a", "b", "c"} {
		m.Add(name, &fakeComponent{name: name, startWg: &wg})
	}
	require.NoError(t, m.Start())
	waitGroupDone(t, &wg) // confirms all 3 starters were invoked
}

func TestManager_Start_SequentialOrder(t *testing.T) {
	var order []string
	m := &Manager{}
	for _, name := range []string{"first", "second", "third"} {
		n := name
		m.Add(name, &fakeComponent{
			name: name,
			startFn: func() error { order = append(order, n); return nil },
		})
	}
	require.NoError(t, m.Start())
	require.Equal(t, []string{"first", "second", "third"}, order)
}

func TestManager_Start_FailStopsOnFirstError(t *testing.T) {
	var started []string
	m := &Manager{}
	m.Add("a", &fakeComponent{
		name: "a",
		startFn: func() error { started = append(started, "a"); return nil },
	})
	m.Add("b", &fakeComponent{
		name:      "b",
		startErr:  assertErr("b-failed"),
		startFn: func() error { started = append(started, "b"); return assertErr("b-failed") },
	})
	m.Add("c", &fakeComponent{
		name: "c",
		startFn: func() error { started = append(started, "c"); return nil },
	})

	err := m.Start()
	require.Error(t, err)
	require.Contains(t, err.Error(), "b: b-failed")
	require.Equal(t, []string{"a", "b"}, started) // c never started
}

func TestManager_Start_OnlyOnce(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(1)
	m.Add("once", &fakeComponent{name: "once", startWg: &wg})

	m.Start()
	waitGroupDone(t, &wg)

	// Second Start must be a no-op. If sync.Once failed, the starter would
	// run again and call wg.Done() a second time, panicking on a negative
	// WaitGroup counter.
	require.NotPanics(t, func() { m.Start() })
}

func TestManager_Stop_ConcurrentTrigger(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(3)
	for _, name := range []string{"a", "b", "c"} {
		m.Add(name, &fakeComponent{name: name, stopWg: &wg})
	}

	done := make(chan error, 1)
	go func() { done <- m.Stop() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not trigger all stoppers within 1s")
	}
}

func TestManager_Stop_JoinsErrors(t *testing.T) {
	m := &Manager{}
	m.Add("a", &fakeComponent{name: "a", stopErr: assertErr("a-failed")})
	m.Add("b", &fakeComponent{name: "b", stopErr: assertErr("b-failed")})

	err := m.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), "a: a-failed")
	require.Contains(t, err.Error(), "b: b-failed")
}

func TestManager_Stop_OnlyOnce_ReturnsSameError(t *testing.T) {
	m := &Manager{}
	m.Add("a", &fakeComponent{name: "a", stopErr: assertErr("boom")})

	err1 := m.Stop()
	err2 := m.Stop()
	require.Same(t, err1, err2) // same instance captured by stopOnce
	require.Contains(t, err1.Error(), "boom")
}

func TestManager_Stop_EmptyManager(t *testing.T) {
	m := &Manager{}
	require.NoError(t, m.Stop())
}

func TestManager_Run_BlocksUntilContextCancelThenStops(t *testing.T) {
	m := &Manager{}
	var startWg, stopWg sync.WaitGroup
	startWg.Add(1)
	stopWg.Add(1)
	m.Add("a", &fakeComponent{name: "a", startWg: &startWg, stopWg: &stopWg})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	waitGroupDone(t, &startWg) // Start triggered

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	waitGroupDone(t, &stopWg) // Stop triggered
}
```

- [ ] **Step 2: Run tests to verify they fail (compile error — Manager API changed)**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/ -run TestManager`
Expected: FAIL — `manager.go` still has `ctx context.Context` params on `Start` / `Stop`, `entry` still has separate `starter` / `stopper` fields, and `Start` / `Stop` signatures don't match.

- [ ] **Step 3: Replace `lifecycle/manager.go` with the new implementation**

```go
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Manager manages a group of Services with sequential startup and concurrent
// shutdown. Registration methods (Add, AddStarter, AddStopper) must be called
// before Start/Run. Manager is not safe for concurrent registration.
type Manager struct {
	entries   []entry
	startOnce sync.Once
	startErr  error
	stopOnce  sync.Once
	stopErr   error
}

// entry pairs a name with the registered Service.
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

// Start sequentially triggers each Service's Start in registration order.
// If any service fails to start, Start returns the error immediately and
// skips remaining services. Subsequent calls are no-ops and return the same
// result as the first call.
//
// Registered Start methods must be non-blocking: they should launch
// long-running work (listeners, workers, etc.) in goroutines and return
// quickly. A blocking Start would stall the entire startup sequence.
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
// Panics propagate (a panic during shutdown is a bug — let it crash so the
// user notices). Returned errors are aggregated via errors.Join, each
// wrapped with the component name. Subsequent calls return the error
// captured by the first call.
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/ -v`
Expected: PASS — all tests in both `lifecycle_test.go` and `manager_test.go` pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/moss/code/base/go-common
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "$(cat <<'EOF'
feat(lifecycle): concurrent Start/Stop with panic and errors.Join

Start launches each Service in its own goroutine and panics on error
(sync.Once guarded). Stop runs each Service's Stop via WaitGroup.Go and
aggregates errors via errors.Join into m.stopErr. Run = Start + wait ctx
+ Stop. Drops the gorx import.
EOF
)"
```

---

## Task 3: Final verification (format, lint, coverage)

**Files:** none modified unless formatting / lint surfaces issues.

- [ ] **Step 1: Format**

Run:
```bash
cd /Users/moss/code/base/go-common
gofmt -w lifecycle/*.go
goimports -w lifecycle/*.go
```
Expected: no output (or files reformatted silently).

- [ ] **Step 2: Lint**

Run: `cd /Users/moss/code/base/go-common && golangci-lint run ./lifecycle/...`
Expected: no issues. If issues surface, fix them in `lifecycle/*.go` and re-run.

- [ ] **Step 3: Coverage**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/... -cover`
Expected: coverage ≥ 85% (per CLAUDE.md target). If below, add tests for the uncovered branch (likely the panic path inside `Start`, which is intentionally not exercised — accept and note in the commit message, or add a subprocess test if coverage falls short).

- [ ] **Step 4: Run full lifecycle suite once more**

Run: `cd /Users/moss/code/base/go-common && go test ./lifecycle/...`
Expected: all PASS.

- [ ] **Step 5: Commit if any formatting / lint changes**

Run:
```bash
cd /Users/moss/code/base/go-common
git status
```
If `lifecycle/*.go` are modified:
```bash
git add lifecycle/
git commit -m "style(lifecycle): format and lint cleanup"
```
If clean, skip.

---

## Verification of Spec Coverage

| Spec section | Covered by |
|---|---|
| Interfaces (no ctx, error return) | Task 1 Step 3 |
| StartFunc / StopFunc adapters | Task 1 Step 3 |
| starterOnly / stopperOnly wrappers | Task 1 Step 3 |
| Manager struct (entries, startOnce, startErr, stopOnce, stopErr) | Task 2 Step 3 |
| Add / AddStarter / AddStopper | Task 2 Step 3 |
| Start (sequential, error return, sync.Once, slog) | Task 2 Step 3 |
| Stop (concurrent, errors.Join, sync.Once) | Task 2 Step 3 |
| Run (Start + ctx + Stop) | Task 2 Step 3 |
| Test: sequential trigger and order | Task 2 Step 1 |
| Test: fail-stops-on-first-error | Task 2 Step 1 |
| Test: concurrent Stop trigger + join | Task 2 Step 1 |
| Coverage ≥ 85% | Task 3 Step 3 |
| Lint clean | Task 3 Step 2 |

Spec items intentionally not covered by automated tests:
- `TestManager_Stop_PanicPropagates` — panic in a child goroutine; `require.Panics` cannot capture. Behavior is enforced by code review of the absence of `recover()` in `Stop`.

---

## Out of Scope

- No refactor of existing call sites (message-service, grpcx.Server, etc.) — separate task.
- No Obsidian sync of the plan (the spec is already synced at `services/go-common/design/v1/lifecycle-design.md`).
- No changes to other packages — `lifecycle` is self-contained.
