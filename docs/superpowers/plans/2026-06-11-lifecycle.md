# Lifecycle Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a `lifecycle` package in go-common providing ordered startup, reverse-order shutdown, rollback, and graceful shutdown for service components.

**Architecture:** Three composable interfaces (Starter, Stopper, Service) + function adapters (StartFunc, StopFunc) + a Manager that registers components and orchestrates lifecycle. Signal handling stays outside the package — callers use `signal.NotifyContext`.

**Tech Stack:** Go 1.25 stdlib (context, errors.Join), testify for tests.

---

## File Structure

```
go-common/lifecycle/
├── lifecycle.go    # Starter, Stopper, Service interfaces + StartFunc/StopFunc adapters
├── manager.go      # Manager type, entry struct, Add/AddStarter/AddStopper/Start/Stop/Run
└── manager_test.go # unit tests with fake components
```

---

## Task 1: Create interfaces and function adapters

**Files:**
- Create: `lifecycle/lifecycle.go`
- Create: `lifecycle/lifecycle_test.go`

- [ ] **Step 1: Write the failing test**

Create `lifecycle/lifecycle_test.go`:

```go
package lifecycle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartFunc_SatisfiesStarter(t *testing.T) {
	var s Starter = StartFunc(func(ctx context.Context) error {
		return nil
	})
	require.NotNil(t, s)
}

func TestStopFunc_SatisfiesStopper(t *testing.T) {
	var s Stopper = StopFunc(func(ctx context.Context) error {
		return nil
	})
	require.NotNil(t, s)
}

func TestStartFunc_InvokesFunction(t *testing.T) {
	called := false
	fn := StartFunc(func(ctx context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, fn.Start(context.Background()))
	require.True(t, called)
}

func TestStopFunc_InvokesFunction(t *testing.T) {
	called := false
	fn := StopFunc(func(ctx context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, fn.Stop(context.Background()))
	require.True(t, called)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./lifecycle/ -run "TestStartFunc|TestStopFunc" -v`
Expected: FAIL — package not found / types undefined

- [ ] **Step 3: Write minimal implementation**

Create `lifecycle/lifecycle.go`:

```go
// Package lifecycle provides ordered startup and reverse-order shutdown
// for service components. Signal handling is left to the caller via
// signal.NotifyContext; this package orchestrates lifecycle only.
package lifecycle

import "context"

// Starter is a component that needs explicit startup.
// Start must be non-blocking: it returns once the component is ready,
// spawning any long-running work in its own goroutine.
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is a component that needs cleanup on shutdown.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Service combines Starter and Stopper for components that need both.
type Service interface {
	Starter
	Stopper
}

// StartFunc adapts a function to the Starter interface.
type StartFunc func(ctx context.Context) error

// Start calls the underlying function.
func (f StartFunc) Start(ctx context.Context) error { return f(ctx) }

// StopFunc adapts a function to the Stopper interface.
type StopFunc func(ctx context.Context) error

// Stop calls the underlying function.
func (f StopFunc) Stop(ctx context.Context) error { return f(ctx) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./lifecycle/ -run "TestStartFunc|TestStopFunc" -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add lifecycle/lifecycle.go lifecycle/lifecycle_test.go
git commit -m "feat(lifecycle): add Starter/Stopper/Service interfaces and func adapters"
```

---

## Task 2: Create Manager struct and Add methods

**Files:**
- Create: `lifecycle/manager.go`
- Create: `lifecycle/manager_test.go`

- [ ] **Step 1: Write the failing test**

Create `lifecycle/manager_test.go`:

```go
package lifecycle

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeComponent records its name to shared slices on Start/Stop,
// allowing tests to verify call order across multiple components.
type fakeComponent struct {
	name       string
	startErr   error
	stopErr    error
	startOrder *[]string
	stopOrder  *[]string
	mu         *sync.Mutex
}

func (f *fakeComponent) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.startOrder = append(*f.startOrder, f.name)
	return f.startErr
}

func (f *fakeComponent) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.stopOrder = append(*f.stopOrder, f.name)
	return f.stopErr
}

func newFakeOrderTracker() (*[]string, *[]string, *sync.Mutex) {
	var startOrder, stopOrder []string
	var mu sync.Mutex
	return &startOrder, &stopOrder, &mu
}

func TestManager_AddService(t *testing.T) {
	m := &Manager{}
	startOrder, _, mu := newFakeOrderTracker()
	comp := &fakeComponent{name: "svc", startOrder: startOrder, stopOrder: startOrder, mu: mu}

	m.Add("svc", comp)

	require.Equal(t, 1, len(m.entries))
	require.Equal(t, "svc", m.entries[0].name)
	require.NotNil(t, m.entries[0].starter)
	require.NotNil(t, m.entries[0].stopper)
}

func TestManager_AddStarter(t *testing.T) {
	m := &Manager{}
	startOrder, stopOrder, mu := newFakeOrderTracker()
	comp := &fakeComponent{name: "starter-only", startOrder: startOrder, stopOrder: stopOrder, mu: mu}

	m.AddStarter("starter-only", comp)

	require.Equal(t, 1, len(m.entries))
	require.NotNil(t, m.entries[0].starter)
	require.Nil(t, m.entries[0].stopper)
}

func TestManager_AddStopper(t *testing.T) {
	m := &Manager{}
	startOrder, stopOrder, mu := newFakeOrderTracker()
	comp := &fakeComponent{name: "stopper-only", startOrder: startOrder, stopOrder: stopOrder, mu: mu}

	m.AddStopper("stopper-only", comp)

	require.Equal(t, 1, len(m.entries))
	require.Nil(t, m.entries[0].starter)
	require.NotNil(t, m.entries[0].stopper)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./lifecycle/ -run "TestManager_Add" -v`
Expected: FAIL — `Manager` type undefined

- [ ] **Step 3: Write minimal implementation**

Create `lifecycle/manager.go`:

```go
package lifecycle

// Manager manages a group of components with ordered startup and
// reverse-order shutdown.
//
// Registration methods (Add, AddStarter, AddStopper) must be called
// before Start/Run. Manager is not safe for concurrent registration.
type Manager struct {
	entries []entry
}

// entry holds one registered component's start/stop handlers.
// Either starter or stopper may be nil.
type entry struct {
	name    string
	starter Starter
	stopper Stopper
}

// Add registers a full Service (both start and stop).
func (m *Manager) Add(name string, svc Service) {
	m.entries = append(m.entries, entry{
		name:    name,
		starter: svc,
		stopper: svc,
	})
}

// AddStarter registers a component that only needs startup (no cleanup).
func (m *Manager) AddStarter(name string, s Starter) {
	m.entries = append(m.entries, entry{
		name:    name,
		starter: s,
		stopper: nil,
	})
}

// AddStopper registers a component that only needs cleanup (no startup).
func (m *Manager) AddStopper(name string, s Stopper) {
	m.entries = append(m.entries, entry{
		name:    name,
		starter: nil,
		stopper: s,
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./lifecycle/ -run "TestManager_Add" -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): add Manager with Add/AddStarter/AddStopper registration"
```

---

## Task 3: Implement Start with ordered startup and rollback

**Files:**
- Modify: `lifecycle/manager.go`
- Modify: `lifecycle/manager_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `lifecycle/manager_test.go`:

```go
func TestManager_Start_OrderIsRegistrationOrder(t *testing.T) {
	m := &Manager{}
	startOrder, _, mu := newFakeOrderTracker()

	for _, name := range []string{"a", "b", "c"} {
		comp := &fakeComponent{name: name, startOrder: startOrder, stopOrder: startOrder, mu: mu}
		m.Add(name, comp)
	}

	require.NoError(t, m.Start(context.Background()))
	require.Equal(t, []string{"a", "b", "c"}, *startOrder)
}

func TestManager_Start_SkipsNilStarters(t *testing.T) {
	m := &Manager{}
	startOrder, _, mu := newFakeOrderTracker()

	m.AddStarter("starter-a", &fakeComponent{name: "starter-a", startOrder: startOrder, stopOrder: startOrder, mu: mu})
	m.AddStopper("stopper-b", &fakeComponent{name: "stopper-b", startOrder: startOrder, stopOrder: startOrder, mu: mu})
	m.AddStarter("starter-c", &fakeComponent{name: "starter-c", startOrder: startOrder, stopOrder: startOrder, mu: mu})

	require.NoError(t, m.Start(context.Background()))
	require.Equal(t, []string{"starter-a", "starter-c"}, *startOrder)
}

func TestManager_Start_RollbackOnFailureStopsAlreadyStarted(t *testing.T) {
	m := &Manager{}
	startOrder, stopOrder, mu := newFakeOrderTracker()

	m.Add("a", &fakeComponent{name: "a", startOrder: startOrder, stopOrder: stopOrder, mu: mu})
	m.Add("b", &fakeComponent{name: "b", startOrder: startOrder, stopOrder: stopOrder, mu: mu})
	m.Add("c", &fakeComponent{
		name:       "c",
		startErr:   assertErr("boom"),
		startOrder: startOrder,
		stopOrder:  stopOrder,
		mu:         mu,
	})

	err := m.Start(context.Background())
	require.Error(t, err)
	require.Equal(t, "boom", err.Error())

	// c was attempted (started), a and b were started before c
	require.Equal(t, []string{"a", "b", "c"}, *startOrder)
	// rollback stops already-started in reverse: c is NOT in stopOrder
	// because its Start failed (it never started successfully — but since
	// our fake records the call before returning the error, "c" appears in
	// startOrder). Rollback should stop a and b (those that returned nil).
	require.Equal(t, []string{"b", "a"}, *stopOrder)
}

func TestManager_Start_EmptyManagerSucceeds(t *testing.T) {
	m := &Manager{}
	require.NoError(t, m.Start(context.Background()))
}
```

Add the helper to `manager_test.go` (near the top, after imports):

```go
// assertErr is a simple error type for test assertions.
type errString string

func (e errString) Error() string { return string(e) }

func assertErr(s string) error { return errString(s) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./lifecycle/ -run "TestManager_Start" -v`
Expected: FAIL — `m.Start` undefined

- [ ] **Step 3: Write minimal implementation**

Add the `context` import to `lifecycle/manager.go`. The file from Task 2 has no imports; add one now:

```go
package lifecycle

import "context"
```

Append the `Start` method after the Add methods:

```go
// Start starts all registered components in registration order.
// If a component fails to start, components that already started are
// stopped in reverse order (rollback), and the original error is returned.
func (m *Manager) Start(ctx context.Context) error {
	var started []entry
	for _, e := range m.entries {
		if e.starter == nil {
			continue
		}
		if err := e.starter.Start(ctx); err != nil {
			// rollback: stop already-started in reverse order
			for i := len(started) - 1; i >= 0; i-- {
				if started[i].stopper != nil {
					_ = started[i].stopper.Stop(ctx)
				}
			}
			return err
		}
		started = append(started, e)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./lifecycle/ -run "TestManager_Start" -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): add Start with ordered startup and rollback"
```

---

## Task 4: Implement Stop with reverse order and error joining

**Files:**
- Modify: `lifecycle/manager.go`
- Modify: `lifecycle/manager_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `lifecycle/manager_test.go`:

```go
func TestManager_Stop_OrderIsReverseRegistration(t *testing.T) {
	m := &Manager{}
	_, stopOrder, mu := newFakeOrderTracker()

	for _, name := range []string{"a", "b", "c"} {
		comp := &fakeComponent{name: name, startOrder: stopOrder, stopOrder: stopOrder, mu: mu}
		m.Add(name, comp)
	}

	require.NoError(t, m.Stop(context.Background()))
	require.Equal(t, []string{"c", "b", "a"}, *stopOrder)
}

func TestManager_Stop_SkipsNilStoppers(t *testing.T) {
	m := &Manager{}
	_, stopOrder, mu := newFakeOrderTracker()

	m.Add("full-a", &fakeComponent{name: "full-a", startOrder: stopOrder, stopOrder: stopOrder, mu: mu})
	m.AddStarter("starter-b", &fakeComponent{name: "starter-b", startOrder: stopOrder, stopOrder: stopOrder, mu: mu})
	m.Add("full-c", &fakeComponent{name: "full-c", startOrder: stopOrder, stopOrder: stopOrder, mu: mu})

	require.NoError(t, m.Stop(context.Background()))
	require.Equal(t, []string{"full-c", "full-a"}, *stopOrder)
}

func TestManager_Stop_ContinuesOnFailureAndJoinsErrors(t *testing.T) {
	m := &Manager{}
	_, stopOrder, mu := newFakeOrderTracker()

	m.Add("a", &fakeComponent{name: "a", startOrder: stopOrder, stopOrder: stopOrder, mu: mu})
	m.Add("b", &fakeComponent{name: "b", stopErr: assertErr("b-failed"), startOrder: stopOrder, stopOrder: stopOrder, mu: mu})
	m.Add("c", &fakeComponent{name: "c", stopErr: assertErr("c-failed"), startOrder: stopOrder, stopOrder: stopOrder, mu: mu})

	err := m.Stop(context.Background())
	require.Error(t, err)

	// all components were stopped despite failures
	require.Equal(t, []string{"c", "b", "a"}, *stopOrder)

	// both errors are joined
	require.Contains(t, err.Error(), "b-failed")
	require.Contains(t, err.Error(), "c-failed")
}

func TestManager_Stop_EmptyManagerSucceeds(t *testing.T) {
	m := &Manager{}
	require.NoError(t, m.Stop(context.Background()))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./lifecycle/ -run "TestManager_Stop" -v`
Expected: FAIL — `m.Stop` undefined

- [ ] **Step 3: Write minimal implementation**

Update `lifecycle/manager.go` import block to add `errors`:

```go
package lifecycle

import (
	"context"
	"errors"
)
```

Append the `Stop` method after `Start`:

```go
// Stop stops all registered components in reverse registration order.
// It continues even if some components fail (best-effort cleanup) and
// returns a combined error via errors.Join.
func (m *Manager) Stop(ctx context.Context) error {
	var errs []error
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if e.stopper == nil {
			continue
		}
		if err := e.stopper.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./lifecycle/ -run "TestManager_Stop" -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): add Stop with reverse order and error joining"
```

---

## Task 5: Implement Run

**Files:**
- Modify: `lifecycle/manager.go`
- Modify: `lifecycle/manager_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `lifecycle/manager_test.go`:

```go
func TestManager_Run_BlocksUntilContextCancelThenStops(t *testing.T) {
	m := &Manager{}
	startOrder, stopOrder, mu := newFakeOrderTracker()

	m.Add("a", &fakeComponent{name: "a", startOrder: startOrder, stopOrder: stopOrder, mu: mu})
	m.Add("b", &fakeComponent{name: "b", startOrder: startOrder, stopOrder: stopOrder, mu: mu})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- m.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	require.Equal(t, []string{"a", "b"}, *startOrder)
	require.Equal(t, []string{"b", "a"}, *stopOrder)
}

func TestManager_Run_ReturnsStartErrorWithoutStopping(t *testing.T) {
	m := &Manager{}
	startOrder, stopOrder, mu := newFakeOrderTracker()

	m.Add("a", &fakeComponent{name: "a", startOrder: startOrder, stopOrder: stopOrder, mu: mu})
	m.Add("b", &fakeComponent{name: "b", startErr: assertErr("start-failed"), startOrder: startOrder, stopOrder: stopOrder, mu: mu})
	m.Add("c", &fakeComponent{name: "c", startOrder: startOrder, stopOrder: stopOrder, mu: mu})

	err := m.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "start-failed")

	// c was never started
	require.NotContains(t, *startOrder, "c")
}
```

Add `"time"` to the import block in `manager_test.go`:

```go
import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./lifecycle/ -run "TestManager_Run" -v`
Expected: FAIL — `m.Run` undefined

- [ ] **Step 3: Write minimal implementation**

Append `Run` to `lifecycle/manager.go`:

```go
// Run starts all components, blocks until ctx is canceled, then stops all.
// Returns the Start error if startup fails, or the Stop result on shutdown.
func (m *Manager) Run(ctx context.Context) error {
	if err := m.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()

	return m.Stop(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./lifecycle/ -run "TestManager_Run" -v`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): add Run for start-block-stop lifecycle"
```

---

## Task 6: Final verification

**Files:**
- No changes

- [ ] **Step 1: Run full test suite for the package**

Run: `go test -race -v ./lifecycle/`
Expected: all 17 tests pass with no race conditions

- [ ] **Step 2: Verify the whole go-common still builds**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./lifecycle/...`
Expected: no errors

- [ ] **Step 4: Verify go.mod / go.sum unchanged**

Run: `git diff go.mod go.sum`
Expected: no changes (lifecycle uses only stdlib + testify which is already a dependency)

- [ ] **Step 5: Verify final git log**

Run: `git log --oneline -6`
Expected: 5 commits from Tasks 1-5, plus this verification step if any doc changes were needed (none expected)
