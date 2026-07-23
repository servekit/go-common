# Shutdown Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two fallback layers on the unhappy shutdown path — `lifecycle.Manager` total Stop timeout (default 10s) and `signalx.RunWithForceQuit` (second signal during Stop sends SIGKILL to self).

**Architecture:** `lifecycle` gains a `NewManager(opts...)` constructor with `WithStopTimeout(d)`. `Manager.Stop()` reads the `stopTimeout` field and, if non-zero, races `wg.Wait()` against `time.After(d)`. `signalx` gains `RunWithForceQuit`, identical to `Run` but spawns `Stop()` in a goroutine and races it against a second signal arrival; on second signal, `syscall.Kill(Getpid(), SIGKILL)`. Existing `Run` and `&Manager{}` zero-value construction are preserved unchanged.

**Tech Stack:** Go 1.25 stdlib (`time`, `sync`, `os/signal`, `syscall`), `errors.Join`, `log/slog`. Tests use `testify/require` + stdlib; one test uses `os/exec` for subprocess SIGKILL verification. No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-06-12-shutdown-reliability-design.md`

---

## File Structure

**Modified files:**

| File | Responsibility |
|---|---|
| `lifecycle/manager.go` | Add `Option` type, `NewManager`, `WithStopTimeout`; add `stopTimeout` field; rewrite `Stop()` to race `wg.Wait()` against timeout |
| `lifecycle/manager_test.go` | Add `stopFn` field to `fakeComponent`; add 4 new tests (default timeout, option applies, timeout fires, zero disables, aggregate errors + timeout) |
| `signalx/signalx.go` | Add `RunWithForceQuit` function |
| `signalx/signalx_test.go` | Add 4 new tests: normal stop on first signal, start error panics, default signals when empty, second signal sends SIGKILL (subprocess) |

**Not modified:**
- `lifecycle/lifecycle.go` — `Starter`, `Stopper`, `Service`, `StartFunc`, `StopFunc`, wrappers unchanged.
- `signalx/signalx.go` `Run` function unchanged.
- `grpcx/server.go` — already implements `signalx.Service`; can opt into force-quit by callers switching to `RunWithForceQuit` at the call site (out of scope here).

---

## Task 1: lifecycle — NewManager constructor + WithStopTimeout option

**Files:**
- Modify: `lifecycle/manager.go` (imports + struct + new functions)
- Modify: `lifecycle/manager_test.go` (new tests)

- [ ] **Step 1: Write the failing tests**

Append to `lifecycle/manager_test.go`:

```go
func TestNewManager_DefaultStopTimeout(t *testing.T) {
	m := NewManager()
	require.Equal(t, 10*time.Second, m.stopTimeout)
}

func TestWithStopTimeout_Applies(t *testing.T) {
	m := NewManager(WithStopTimeout(5 * time.Second))
	require.Equal(t, 5*time.Second, m.stopTimeout)
}

func TestWithTimeout_ZeroDisables(t *testing.T) {
	m := NewManager(WithStopTimeout(0))
	require.Equal(t, time.Duration(0), m.stopTimeout)
}
```

Add `"time"` to the test file's import block if not present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./lifecycle/ -run 'TestNewManager_DefaultStopTimeout|TestWithStopTimeout_Applies|TestWithTimeout_ZeroDisables' -v`

Expected: FAIL with `undefined: NewManager` (and `undefined: WithStopTimeout`).

- [ ] **Step 3: Add `time` import and `stopTimeout` field to Manager struct**

In `lifecycle/manager.go`, update imports:

```go
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)
```

Update the `Manager` struct (add the `stopTimeout` field at the end):

```go
type Manager struct {
	entries     []entry
	startOnce   sync.Once
	startErr    error
	stopOnce    sync.Once
	stopErr     error
	stopTimeout time.Duration
}
```

- [ ] **Step 4: Add Option type, NewManager, WithStopTimeout**

Append after the `entry` type definition (or anywhere top-level in `lifecycle/manager.go`):

```go
// Option configures a Manager at construction time.
type Option func(*Manager)

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./lifecycle/ -run 'TestNewManager_DefaultStopTimeout|TestWithStopTimeout_Applies|TestWithTimeout_ZeroDisables' -v`

Expected: PASS (3 tests).

- [ ] **Step 6: Run full package tests to verify no regression**

Run: `go test ./lifecycle/ -v`

Expected: All tests PASS (existing tests still use `&Manager{}`, which has zero `stopTimeout` — current behavior preserved).

- [ ] **Step 7: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): add NewManager constructor with WithStopTimeout option

NewManager + Option pattern sets the foundation for Manager.Stop
total-budget timeout. Default StopTimeout is 10s; &Manager{} zero
value remains 0 (unlimited) for backward compatibility. Stop
behavior is still unchanged in this commit — the field is read in
the next commit."
```

---

## Task 2: lifecycle — Stop timeout enforcement

**Files:**
- Modify: `lifecycle/manager.go` (`Stop` method)
- Modify: `lifecycle/manager_test.go` (`fakeComponent` gains `stopFn`; new tests)

- [ ] **Step 1: Add `stopFn` field to `fakeComponent`**

In `lifecycle/manager_test.go`, update the `fakeComponent` struct:

```go
type fakeComponent struct {
	name     string
	startErr error
	stopErr  error
	startWg  *sync.WaitGroup // optional: Done() on Start
	stopWg   *sync.WaitGroup // optional: Done() on Stop
	startFn  func() error    // optional: custom Start logic
	stopFn   func() error    // optional: custom Stop logic
}
```

Update the `Stop()` method on `fakeComponent`:

```go
func (f *fakeComponent) Stop() error {
	if f.stopWg != nil {
		f.stopWg.Done()
	}
	if f.stopFn != nil {
		return f.stopFn()
	}
	return f.stopErr
}
```

- [ ] **Step 2: Write the failing tests**

Append to `lifecycle/manager_test.go`:

```go
func TestManager_Stop_TimesOut(t *testing.T) {
	m := NewManager(WithStopTimeout(50 * time.Millisecond))
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(time.Second); return nil },
	})

	start := time.Now()
	err := m.Stop()
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout after")
	require.Less(t, elapsed, 500*time.Millisecond, "Stop should return shortly after timeout, not wait for the slow service")
}

func TestManager_Stop_NoTimeoutWhenZero(t *testing.T) {
	m := &Manager{} // zero-value, stopTimeout == 0
	done := make(chan struct{})
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(100 * time.Millisecond); close(done); return nil },
	})

	err := m.Stop()
	require.NoError(t, err)
	select {
	case <-done:
		// Stop waited for the slow service to finish — no timeout.
	default:
		t.Fatal("Stop returned before slow service finished; zero-value timeout was not respected")
	}
}

func TestManager_Stop_AggregatesErrorsAndTimeout(t *testing.T) {
	m := NewManager(WithStopTimeout(50 * time.Millisecond))
	m.Add("fast-err", &fakeComponent{
		name:    "fast-err",
		stopErr: assertErr("fast failure"),
	})
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(time.Second); return nil },
	})

	err := m.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), "fast-err: fast failure")
	require.Contains(t, err.Error(), "timeout after")
}

func TestManager_Stop_NoTimeoutWhenAllFast(t *testing.T) {
	m := NewManager(WithStopTimeout(time.Second))
	m.Add("a", &fakeComponent{name: "a"})
	m.Add("b", &fakeComponent{name: "b"})

	err := m.Stop()
	require.NoError(t, err)
}
```

Use the existing `assertErr` helper (defined in `lifecycle/lifecycle_test.go`) — no new imports needed in `manager_test.go`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./lifecycle/ -run 'TestManager_Stop_TimesOut|TestManager_Stop_NoTimeoutWhenZero|TestManager_Stop_AggregatesErrorsAndTimeout|TestManager_Stop_NoTimeoutWhenAllFast' -v`

Expected: 
- `TestManager_Stop_TimesOut`: FAIL — Stop blocks for ~1s and returns nil.
- `TestManager_Stop_AggregatesErrorsAndTimeout`: FAIL — error missing the `"timeout after"` substring.
- Others may pass coincidentally — focus on the timeout assertion failures.

- [ ] **Step 4: Rewrite Stop() with timeout race**

Replace the entire body of `Manager.Stop()` in `lifecycle/manager.go`:

```go
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

		if timedOut {
			errs = append(errs, fmt.Errorf("lifecycle: stop timeout after %s", m.stopTimeout))
		}
		m.stopErr = errors.Join(errs...)
	})
	return m.stopErr
}
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./lifecycle/ -run 'TestManager_Stop_TimesOut|TestManager_Stop_NoTimeoutWhenZero|TestManager_Stop_AggregatesErrorsAndTimeout|TestManager_Stop_NoTimeoutWhenAllFast' -v`

Expected: PASS (4 tests).

- [ ] **Step 6: Run the full lifecycle test suite**

Run: `go test ./lifecycle/ -v`

Expected: All tests PASS.

- [ ] **Step 7: Run coverage check**

Run: `go test ./lifecycle/ -cover`

Expected: Coverage remains at or near the previous level (target 85% per CLAUDE.md). The new branch (timeout vs. waitDone) is covered by `TimesOut` and `NoTimeoutWhenAllFast`.

- [ ] **Step 8: Commit**

```bash
git add lifecycle/manager.go lifecycle/manager_test.go
git commit -m "feat(lifecycle): enforce Stop total-budget timeout

Manager.Stop now races wg.Wait() against time.After(stopTimeout).
When timeout fires, the returned error includes a 'lifecycle: stop
timeout after Xs' entry alongside any per-service errors.

Zero-value &Manager{} preserves the previous unlimited-wait behavior.
NewManager() defaults to 10s.

fakeComponent gains a stopFn field for custom Stop logic in
order-dependent tests. The background wg.Wait goroutine continues
running after timeout — Go cannot kill goroutines; this is the
accepted trade-off documented in the spec."
```

---

## Task 3: signalx — RunWithForceQuit basic happy path

**Files:**
- Modify: `signalx/signalx.go` (new function)
- Modify: `signalx/signalx_test.go` (new tests, reuse `fakeService` + helpers)

- [ ] **Step 1: Write the failing tests**

Append to `signalx/signalx_test.go`:

```go
func TestRunWithForceQuit_StopsNormallyOnFirstSignal(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	go func() { _ = RunWithForceQuit(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop", stopped)
}

func TestRunWithForceQuit_StartError_PanicsWithWrap(t *testing.T) {
	startErr := errors.New("init failed")
	s := &fakeService{
		startFn: func() error { return startErr },
	}

	require.PanicsWithValue(t, "signalx: start failed: init failed", func() {
		_ = RunWithForceQuit(s)
	})
}

func TestRunWithForceQuit_DefaultSignals_WhenEmpty(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	go func() { _ = RunWithForceQuit(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop", stopped)
}
```

No new imports needed — `fakeService`, `registerSafetyNet`, `pollSendSignal`, `await`, and `errors` are already imported in `signalx/signalx_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./signalx/ -run 'TestRunWithForceQuit_StopsNormallyOnFirstSignal|TestRunWithForceQuit_StartError_PanicsWithWrap|TestRunWithForceQuit_DefaultSignals_WhenEmpty' -v`

Expected: FAIL with `undefined: RunWithForceQuit`.

- [ ] **Step 3: Implement RunWithForceQuit**

Append to `signalx/signalx.go` (below the existing `Run` function):

```go
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

	stopDone := make(chan error, 1)
	go func() { stopDone <- s.Stop() }()

	select {
	case err := <-stopDone:
		return err
	case <-ch: // second signal during Stop → SIGKILL self
		syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
		return nil // unreachable; SIGKILL cannot be caught
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./signalx/ -run 'TestRunWithForceQuit_StopsNormallyOnFirstSignal|TestRunWithForceQuit_StartError_PanicsWithWrap|TestRunWithForceQuit_DefaultSignals_WhenEmpty' -v`

Expected: PASS (3 tests).

- [ ] **Step 5: Run the full signalx test suite**

Run: `go test ./signalx/ -v`

Expected: All tests PASS (including existing `Run_*` tests).

- [ ] **Step 6: Commit**

```bash
git add signalx/signalx.go signalx/signalx_test.go
git commit -m "feat(signalx): add RunWithForceQuit for SIGKILL on second signal

Like Run, but a second signal during Stop triggers
syscall.Kill(Getpid(), SIGKILL), terminating the process
immediately (exit code 137). Channel buffer is 2 to avoid
dropping a rapid double-tap. Run is unchanged; callers opt
into force-quit policy explicitly."
```

---

## Task 4: signalx — Second-signal SIGKILL verification via subprocess

**Files:**
- Modify: `signalx/signalx_test.go` (one new test, no production code change)

This test cannot run in-process — the test process would be killed. Standard pattern: parent spawns child via `os/exec`, child enters `RunWithForceQuit` with a Stop that blocks forever, parent sends two signals, parent asserts child died via SIGKILL.

- [ ] **Step 1: Update signalx_test.go imports**

The subprocess test needs `bytes` and `os/exec` (not currently imported). Update the import block to:

```go
import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"code.byteflowing.com/base/go-common/lifecycle"
)
```

- [ ] **Step 2: Write the test**

Append to `signalx/signalx_test.go`:

```go
// TestRunWithForceQuit_SecondSignalSendsSIGKILL verifies the core contract
// of RunWithForceQuit: a second signal during Stop terminates the process
// via SIGKILL.
//
// Cannot run in-process — the test process would be killed. Parent mode
// spawns a child via os/exec with env SHUTDOWN_RELIABILITY_SUBPROCESS=1.
// Child mode constructs a lifecycle.Manager whose only Service blocks
// forever in Stop, then calls RunWithForceQuit. Parent sends SIGTERM
// twice and asserts the child's WaitStatus shows SIGKILL.
func TestRunWithForceQuit_SecondSignalSendsSIGKILL(t *testing.T) {
	if os.Getenv("SHUTDOWN_RELIABILITY_SUBPROCESS") == "1" {
		// Child mode: enter RunWithForceQuit with a blocking Stop.
		m := lifecycle.NewManager()
		m.Add("blocked", lifecycle.StopFunc(func() {
			select {} // block forever
		}))
		_ = RunWithForceQuit(m)
		return
	}

	// Parent mode: spawn a fresh child running this same test.
	cmd := exec.Command(os.Args[0],
		"-test.run=^TestRunWithForceQuit_SecondSignalSendsSIGKILL$",
		"-test.v")
	cmd.Env = append(os.Environ(), "SHUTDOWN_RELIABILITY_SUBPROCESS=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	// Give the child time to reach the select{} in Stop.
	// 500ms is generous; signal.Notify + Start + first signal dispatch
	// + goroutine entry to Stop is well under 100ms in practice.
	time.Sleep(500 * time.Millisecond)

	// First signal → triggers Stop (which blocks forever).
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	// Second signal a moment later → triggers SIGKILL.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))

	err := cmd.Wait()
	require.Error(t, err, "child should have been killed; stdout=%s stderr=%s",
		stdout.String(), stderr.String())

	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "expected *exec.ExitError, got %T: %v", err, err)

	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok, "expected syscall.WaitStatus inside ExitError")
	require.True(t, ws.Signaled(),
		"expected child terminated by signal; exit status %d, stdout=%s stderr=%s",
		ws.ExitStatus(), stdout.String(), stderr.String())
	require.Equal(t, syscall.SIGKILL, ws.Signal(),
		"expected SIGKILL, got %v; stdout=%s stderr=%s",
		ws.Signal(), stdout.String(), stderr.String())
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./signalx/ -run 'TestRunWithForceQuit_SecondSignalSendsSIGKILL' -v`

Expected: PASS.

If the test fails with "expected SIGKILL, got SIGTERM" or similar, the second signal arrived before `signal.Notify` was registered (race). Increase the initial `time.Sleep` to 1s. If it fails with timeout, the child did not get killed — verify `select{}` is in the Stop path.

- [ ] **Step 4: Run the full signalx test suite**

Run: `go test ./signalx/ -v`

Expected: All tests PASS, including the subprocess test.

- [ ] **Step 5: Run coverage**

Run: `go test ./signalx/ -cover`

Expected: Coverage should remain high; the unreachable `return nil` after `syscall.Kill` may show as uncovered — that's expected and acceptable (add `//nolint:unused` if the linter complains).

- [ ] **Step 6: Commit**

```bash
git add signalx/signalx_test.go
git commit -m "test(signalx): verify second signal triggers SIGKILL via subprocess

The RunWithForceQuit force-quit path cannot be tested in-process
because the test process would be killed. Standard subprocess
pattern: parent spawns child, sends two SIGTERMs, asserts child
exited via SIGKILL (WaitStatus.Signaled && Signal == SIGKILL).

Child uses lifecycle.Manager + StopFunc(select{}) to block Stop
indefinitely, exercising the second-signal path."
```

---

## Task 5: Lint and final verification

**Files:** None modified.

- [ ] **Step 1: Run gofmt**

Run: `gofmt -w lifecycle/manager.go lifecycle/manager_test.go signalx/signalx.go signalx/signalx_test.go`

Expected: No output (or files reformatted in place; no semantic change).

- [ ] **Step 2: Run goimports** (if available)

Run: `goimports -w lifecycle/manager.go lifecycle/manager_test.go signalx/signalx.go signalx/signalx_test.go`

Expected: No diff.

- [ ] **Step 3: Run golangci-lint**

Run: `golangci-lint run ./lifecycle/ ./signalx/`

Expected: No issues. If `staticcheck` flags the unreachable `return nil` after `syscall.Kill`, suppress inline with a comment.

- [ ] **Step 4: Run all package tests**

Run: `go test ./... -cover`

Expected: All packages PASS. Coverage for `lifecycle` and `signalx` meets the 85% target per CLAUDE.md.

- [ ] **Step 5: Run race detector**

Run: `go test -race ./lifecycle/ ./signalx/`

Expected: PASS with no race warnings. Critical here because `Stop` now races `wg.Wait()` against `time.After`, and the new goroutine accessing `errs` must be properly synchronized.

- [ ] **Step 6: No commit needed if no changes**

If gofmt/goimports/lint produced changes, commit them:

```bash
git add lifecycle/ signalx/
git commit -m "style: gofmt and lint cleanup for shutdown reliability"
```

Otherwise skip this step.

---

## Task 6: Sync plan to Obsidian

Per CLAUDE.md, every design/plan document must be mirrored to the Obsidian vault.

> **Note:** This plan was already synced to Obsidian at write-time (2026-06-12). Re-run these steps only if the plan was modified during implementation; otherwise skip to verification (Step 5).

- [ ] **Step 1: Copy plan to Obsidian vault**

```bash
cp docs/superpowers/plans/2026-06-12-shutdown-reliability.md \
   "$HOME/Library/Mobile Documents/iCloud~md~obsidian/Documents/only/services/go-common/plan/v1/5-shutdown-reliability.md"
```

- [ ] **Step 2: Add `## 关联` section at the end**

Edit the Obsidian copy (`.../plan/v1/5-shutdown-reliability.md`), appending:

```markdown

## 关联

**对应设计：** [[services/go-common/design/v1/shutdown-reliability-design|shutdown-reliability-design]]
```

- [ ] **Step 3: Update `services/go-common/go-common.md` plan table**

Edit the Obsidian file `services/go-common/go-common.md`, adding one row to the "开发计划 (plan/v1)" table after the 4-lifecycle row:

```markdown
| [[services/go-common/plan/v1/5-shutdown-reliability\|5-shutdown-reliability]] | 关闭可靠性实施计划（StopTimeout + 二次信号强退） |
```

- [ ] **Step 4: Append to `services/changes.md`**

```bash
obsidian vault=only append file="services/changes" \
  content=$'\n- 2026-06-12: 新增 services/go-common/plan/v1/5-shutdown-reliability.md — 关闭可靠性实施计划'
```

- [ ] **Step 5: Verify in Obsidian**

Open Obsidian, navigate to `services/go-common/plan/v1/5-shutdown-reliability.md`, confirm the backlink to the design doc resolves (no broken-link indicator).

- [ ] **Step 6: No git commit**

Obsidian vault lives outside the repo. No git operations needed for this task.

---

## Post-Implementation Notes

### Backward compatibility verification

After all tasks complete, verify these existing call patterns still compile and behave identically:

```go
// Pattern 1: zero-value Manager (existing code, unchanged behavior)
m := &lifecycle.Manager{}
m.Add("svc", svc)
m.Run(ctx) // StopTimeout = 0, no timeout enforcement

// Pattern 2: existing signalx.Run (unchanged)
signalx.Run(m) // no force-quit; second signal ignored during Stop

// Pattern 3: existing grpcx.Server (unchanged)
srv := grpcx.New(cfg, reg, nil)
srv.Run() // still works; uses signalx.Run internally
```

### Updated Non-Goals in the 2026-06-11 specs

The 2026-06-11 specs explicitly stated:
- "No graceful-shutdown timeout at the Manager level" (lifecycle-concurrent-design.md)
- "No second-signal force-quit" (signalx-design.md)

These were revised in the 2026-06-12 spec. After implementation, the 2026-06-11 spec files should be updated to reflect this — but that is a documentation-only follow-up, not part of the implementation plan.

### Three-layer defense (operational summary)

After this plan is executed, a service using `lifecycle.NewManager()` + `signalx.RunWithForceQuit(m)` gets:

1. **Per-service timeout** — Service authors implement `context.WithTimeout` inside individual `Stop()` methods (out of scope for this plan).
2. **Manager total budget** — `WithStopTimeout` default 10s, configurable per Manager.
3. **Signal-driven force-quit** — Second SIGTERM/SIGINT during Stop → SIGKILL self.
4. **Orchestrator backstop** — K8s `terminationGracePeriodSeconds` → external SIGKILL (out of scope).
