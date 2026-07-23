# Shutdown Reliability: Stop Timeout + Force-Quit on Second Signal

Date: 2026-06-12

Supplements `2026-06-11-lifecycle-concurrent-design.md` and
`2026-06-11-signalx-design.md`. Those specs documented the "happy path" —
graceful shutdown assuming every `Stop()` returns in reasonable time.
This spec adds two fallback layers for the unhappy path:

1. **lifecycle**: a total budget on `Manager.Stop()`, so a stuck service
   cannot keep the process alive indefinitely.
2. **signalx**: a force-quit path, so a second signal during shutdown
   terminates the process immediately (via `SIGKILL`).

## Background

The existing shutdown pipeline has two known weaknesses:

**Weakness 1 — no total budget on Stop.** `Manager.Stop()` blocks on
`wg.Wait()` with no timeout. If any registered `Service.Stop()` hangs
(deadlock, stuck syscall, slow downstream), the process never exits
through this path. In production, Kubernetes/Docker eventually send
`SIGKILL` after `terminationGracePeriodSeconds` (default 30s), but the
in-between state is opaque: no log, no error, no attribution.

**Weakness 2 — no escape hatch during graceful shutdown.** A user who
hits Ctrl+C once and realizes the service is stuck has no way to say
"stop trying, just exit now" from inside the program. They have to
open another shell and `kill -9`. The signalx spec explicitly listed
this as a non-goal; this spec revisits that decision and provides an
opt-in API.

## Goal

Add two independent, composable mechanisms:

- `lifecycle.NewManager(opts...)` with `WithStopTimeout(d)`. Default 10s.
  `Manager.Stop()` returns aggregated errors + a timeout marker if the
  total budget is exceeded.
- `signalx.RunWithForceQuit(s, sigs...)`. Same as `Run`, plus: a second
  signal during `Stop()` triggers `syscall.Kill(Getpid(), SIGKILL)`.

## Non-Goals (revised)

In `2026-06-11-signalx-design.md` line 30 stated:

> No logging, no os.Exit, no second-signal force-quit. Caller decides policy.

This spec **partially revises** that statement:

- `signalx.Run` continues to honor the original promise — zero change,
  zero force-quit behavior.
- `signalx.RunWithForceQuit` is a new, explicitly opt-in API for callers
  who want force-quit policy. Choosing it means choosing to let signalx
  terminate the process on second signal.

The "no logging" and "no os.Exit" rules still hold for both functions.
`RunWithForceQuit` uses `syscall.Kill` (kernel-driven termination), not
`os.Exit` (Go runtime-driven).

In `2026-06-11-lifecycle-concurrent-design.md` lines 299-300 stated:

> No graceful-shutdown timeout at the Manager level. Components implement
> their own timeout inside Stop() (e.g. context.WithTimeout) if needed.

This spec **revises** that statement: Manager now provides a total
budget as a backstop. Per-component timeouts inside individual `Stop()`
methods remain the recommended first line of defense; Manager timeout
is the aggregate backstop, not a replacement.

## Three-Layer Defense

```
SIGTERM ──→ signalx.Run[WithForceQuit] receives first signal
         │
         ├─→ m.Stop() starts
         │     ├─ Each Service.Stop() runs concurrently
         │     │   └─ Service-internal context.WithTimeout (per-component)
         │     │
         │     ├─ lifecycle.WithStopTimeout (aggregate, default 10s)
         │     │
         │     └─ (during Stop) second signal arrives
         │           │
         │           └─→ signalx.RunWithForceQuit → SIGKILL → exit 137
         │
         └─ External SIGKILL (K8s terminationGracePeriodSeconds) — final fallback
```

| Layer | Scope | Mechanism | Owner |
|---|---|---|---|
| 1. Per-service | Single component | `context.WithTimeout` inside each `Stop()` | Service author |
| 2. Manager total | All services in one Manager | `WithStopTimeout` (default 10s) | Manager caller |
| 3. Signal force-quit | Whole process | `syscall.Kill(SIGKILL)` on 2nd signal | signalx caller |
| 4. Orchestrator | Container/pod | `terminationGracePeriodSeconds` → SIGKILL | Deployment |

Layers 1 and 4 are out of scope for this spec but listed for completeness.
This spec adds layers 2 and 3.

---

## Part 1: lifecycle — Stop Timeout

### Configuration API

```go
package lifecycle

import "time"

// Option configures a Manager at construction time.
type Option func(*Manager)

// NewManager constructs a Manager with the given options applied.
// Default StopTimeout is 10 seconds; pass WithStopTimeout(0) to disable.
func NewManager(opts ...Option) *Manager {
    m := &Manager{stopTimeout: 10 * time.Second}
    for _, opt := range opts {
        opt(m)
    }
    return m
}

// WithStopTimeout sets the total budget for Manager.Stop().
// 0 disables the timeout (equivalent to &Manager{} zero-value behavior).
func WithStopTimeout(d time.Duration) Option {
    return func(m *Manager) { m.stopTimeout = d }
}
```

**Field visibility**: `stopTimeout` is unexported. The only way to set it
is through `NewManager` + `WithStopTimeout`. This prevents callers from
mutating the field after `Stop()` has begun (which would be a data race
and semantically meaningless — the timeout is captured at Stop start).

**Zero-value compatibility**: `&Manager{}` continues to work. Zero-value
`stopTimeout` is 0, meaning "no timeout" — identical to current behavior.
Existing call sites need not change.

### Manager struct

```go
type Manager struct {
    entries     []entry
    startOnce   sync.Once
    startErr    error
    stopOnce    sync.Once
    stopErr     error
    stopTimeout time.Duration  // 0 = unlimited
}
```

### Stop behavior

```go
func (m *Manager) Stop() error {
    m.stopOnce.Do(func() {
        var wg sync.WaitGroup
        var mu sync.Mutex
        var errs []error

        for _, e := range m.entries {
            // Same panic-safety guarantee as before: wg.Go runs defer
            // wg.Done() before re-panicking, so wg.Wait() cannot deadlock
            // if a Stop panics.
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

        // The mutex here must cover the entire post-select snapshot/append/Join
        // sequence, not just the append above. After timeout fires, background
        // wg.Go writers may still be appending to errs (Go cannot kill goroutines);
        // reading errs without the lock would race with them.
        mu.Lock()
        if timedOut {
            errs = append(errs, fmt.Errorf("lifecycle: stop timeout after %s", m.stopTimeout))
        }
        m.stopErr = errors.Join(errs...)
        mu.Unlock()
    })
    return m.stopErr
}
```

**Notes on the goroutine that wraps `wg.Wait()`**:

- It is intentionally separate from the per-service goroutines spawned by
  `wg.Go`. That wrapper goroutine has no `defer` that the outer code needs;
  it just closes `waitDone` when all service Stops return.
- After timeout, `wg.Wait()` continues blocking in the background. The
  wrapper goroutine stays alive until all Stops eventually return (or the
  process exits). This is the unavoidable Go trade-off: we cannot kill
  goroutines.
- `slog.Warn` is the only log added in this package. Rationale: a timeout
  during shutdown is operationally significant and not visible through
  returned errors alone (the caller may ignore the return value). The
  one-line warning is the minimum viable observability. This does not
  violate the project rule "基础库原则上不写日志" — the rule's intent is
  "don't substitute logging for proper error returns", which is not the
  case here (errors are still returned).

**Error shape**:

- Normal completion: `errors.Join` of per-service errors, each wrapped
  as `"component: error"`. Same as today.
- Timeout: above, plus one extra entry `"lifecycle: stop timeout after 10s"`.
- Timeout with no per-service errors: returns just the timeout message
  (non-nil, so callers can detect).
- No sentinel error variable. Callers detecting timeout by string match
  is acceptable; if a real need emerges later, add `ErrStopTimeout` and
  retroactively wrap.

### Run interaction

`Run(ctx)` calls `Stop()` internally. The new `stopTimeout` field is read
inside `Stop()`, so `Run` requires no signature change. A `Manager` built
via `NewManager()` and passed to `signalx.Run` will have `StopTimeout=10s`
applied automatically when signalx invokes `Stop()`.

### Backward compatibility

| Construction | stopTimeout | Behavior |
|---|---|---|
| `&Manager{}` (existing code) | 0 | No timeout (current behavior preserved) |
| `NewManager()` | 10s | Default timeout applied |
| `NewManager(WithStopTimeout(0))` | 0 | Explicitly disabled |
| `NewManager(WithStopTimeout(30*time.Second))` | 30s | Custom |

`Manager.Add`, `AddStarter`, `AddStopper`, `Start`, `Stop`, `Run` signatures
are unchanged.

### Testing

| Test | What it verifies |
|---|---|
| `TestNewManager_DefaultStopTimeout` | `NewManager()` yields `stopTimeout == 10s` |
| `TestWithStopTimeout_Applies` | `NewManager(WithStopTimeout(5*time.Second))` yields `stopTimeout == 5s` |
| `TestManager_Stop_TimesOut` | One fake `Stop` sleeps 1s; `StopTimeout=50ms`; returned error contains `"timeout after"` |
| `TestManager_Stop_NoTimeoutWhenZero` | `&Manager{}` with a slow `Stop` (100ms) waits and returns nil — preserves backward compatibility |
| `TestManager_Stop_AggregatesErrorsAndTimeout` | One `Stop` errors immediately, another sleeps past timeout; aggregated error contains both the per-service error and the timeout marker |
| `TestManager_Stop_NoTimeoutWhenAllFast` | All `Stop` calls complete within budget; returns nil (or just per-service errors) with no timeout marker |
| `TestManager_Run_AppliesStopTimeout` | End-to-end: `Run(ctx)` with a cancel after Start, slow `Stop`, verify Run returns the timeout error |

Tests use fake components (`StartFunc`/`StopFunc`) with `time.Sleep` — no
external dependencies.

---

## Part 2: signalx — Force-Quit on Second Signal

### API

```go
package signalx

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
)

// RunWithForceQuit is like Run but, if a second signal arrives while the
// service is stopping, immediately sends SIGKILL to the current process.
//
// Use when callers want a second Ctrl+C (or external kill during graceful
// shutdown) to forcefully terminate without waiting for Stop to complete.
// defer calls do not run after SIGKILL — same as an external kill -9.
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

    ch := make(chan os.Signal, 2)  // buffer 2 so rapid double-tap doesn't drop
    signal.Notify(ch, sigs...)
    defer signal.Stop(ch)

    <-ch  // First signal → trigger Stop.

    stopDone := make(chan error, 1)
    go func() { stopDone <- s.Stop() }()

    select {
    case err := <-stopDone:
        return err
    case <-ch:  // Second signal during Stop → SIGKILL self.
        syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
        return nil  // unreachable; SIGKILL cannot be caught
    }
}
```

### Key decisions

| Decision | Choice | Reason |
|---|---|---|
| `Run` signature/behavior | unchanged | Existing callers see zero change |
| New API name | `RunWithForceQuit` | Explicit about the policy being opted into |
| Second-signal type | any configured signal | Matches K8s/Docker convention; user pressing Ctrl+C twice OR external kill during graceful shutdown both count |
| Trigger action | `syscall.Kill(Getpid(), SIGKILL)` | Mimics external `kill -9`; kernel-driven, exit code 137; K8s recognizes the SIGKILL termination |
| Channel buffer | 2 | Two rapid Ctrl+C presses must not lose the second signal |
| Trigger window | only during Stop | Start-phase signals are not registered (consistent with existing `Run`) |
| Exit code | 137 (128 + 9) | Produced by the kernel on SIGKILL; signalx does not set it |
| `os.Exit` usage | never | Kernel handles termination via signal, not Go runtime |

### Why `syscall.Kill` instead of `os.Exit`

Both end the process. Differences:

| Aspect | `syscall.Kill(Getpid(), SIGKILL)` | `os.Exit(1)` |
|---|---|---|
| Driven by | kernel | Go runtime |
| Exit code | 137 | 1 |
| `kubectl describe pod` shows | "SIGKILL" | generic non-zero exit |
| Signal handler interception | no (SIGKILL cannot be caught) | n/a |
| Aligns with external force-quit semantics | yes | no |

`syscall.Kill` is more honest about what's happening: the program is being
killed, not exiting. K8s operators reading logs/pod state see consistent
behavior whether the kill came from inside the process or outside.

### Channel buffer rationale

The naive `make(chan os.Signal, 1)` from `Run` is insufficient here.
`signal.Notify` drops signals if the channel is full at delivery time.
With buffer 1:

1. First Ctrl+C delivered, channel now empty → received, triggers Stop.
2. Second Ctrl+C delivered before the first `<-ch` is consumed again —
   but the first consumption already happened, and the second `<-ch`
   in the `select` is ready. So buffer 1 is technically enough **if the
   select is reached before the second signal arrives**.

The race: between `<-ch` (first) and entering the `select`, the goroutine
is executing `go func() { stopDone <- s.Stop() }()`. This is fast but not
instant. A user holding down Ctrl+C could fire the second signal during
that window. Buffer 2 ensures delivery.

### Why not `signal.NotifyContext`

`signal.NotifyContext` returns a `context.Context` that gets canceled on
first signal. It's elegant for the first signal but offers no help for
the second — the context is already canceled. We'd need to register a
fresh `signal.Notify` afterward, which is more code than the channel-based
approach above.

### Testing

| Test | What it verifies |
|---|---|
| `RunWithForceQuit_StopsNormallyOnFirstSignal` | One signal → `Stop` invoked → return value is `Stop`'s error |
| `RunWithForceQuit_SecondSignalSendsSIGKILL` | Subprocess: child calls `RunWithForceQuit` with a fake `Stop` that blocks; parent sends two signals; child exit code is 137 (-1 on Unix because `os.Exit` was not called — the process was killed) |
| `RunWithForceQuit_StartError_Panics` | Same as `Run_StartError_PanicsWithWrap` |
| `RunWithForceQuit_DefaultSignals_WhenEmpty` | Same as `Run_DefaultSignals_WhenEmpty` |

**SIGKILL test approach**: the test cannot run in-process — the process
would be killed. Use the `TestMain` + subprocess pattern:

```go
func TestRunWithForceQuit_SecondSignalSendsSIGKILL(t *testing.T) {
    if os.Getenv("FORCE_QUIT_SUBPROCESS") == "1" {
        // Child mode: run with a blocking Stop, expect to be SIGKILLed.
        m := lifecycle.NewManager()
        m.Add("blocked", lifecycle.StopFunc(func() {
            select {}  // block forever
        }))
        _ = signalx.RunWithForceQuit(m)
        return
    }

    // Parent mode: spawn child, send two signals, check exit code.
    cmd := exec.Command(os.Args[0], "-test.run=TestRunWithForceQuit_SecondSignalSendsSIGKILL")
    cmd.Env = append(os.Environ(), "FORCE_QUIT_SUBPROCESS=1")
    // ... start, send SIGTERM twice, wait, assert exit status is SIGKILL
}
```

The `select {}` in the fake Stop blocks forever — neither returns nor
times out (lifecycle's `StopTimeout` would also fire, but signalx's
second-signal path wins because the test sends signals faster than 10s).
Assertion: child was killed by SIGKILL (`ProcessState.Sys().(syscall.WaitStatus).Signaled() == true` and `.Signal() == syscall.SIGKILL`).

---

## Part 3: How They Compose

### Default usage (recommended)

```go
m := lifecycle.NewManager()  // StopTimeout=10s default
m.Add("grpc", grpcService)
m.Add("worker", workerService)

if err := signalx.RunWithForceQuit(m); err != nil {
    slog.Error("shutdown error", "error", err)
    os.Exit(1)
}
```

Shutdown timeline on `SIGTERM`:

- t=0: SIGTERM received by `RunWithForceQuit`
- t=0: `m.Stop()` starts; all service `Stop()`s run concurrently
- t=0..Ts: per-service `Stop()` returns (Ts = slowest service, hopefully < 10s)
- If Ts ≤ 10s: `Stop()` returns, `RunWithForceQuit` returns aggregated errors
- If Ts > 10s: `Stop()` returns at t=10s with timeout error; `RunWithForceQuit` returns
- If user sends a second signal at any t in [0, Ts]: SIGKILL → exit 137

### Compatibility matrix

| Caller uses | First-signal graceful stop | Total 10s budget | Force-quit on 2nd signal |
|---|---|---|---|
| `&Manager{}` + `signalx.Run` | yes | no | no |
| `&Manager{}` + `signalx.RunWithForceQuit` | yes | no | yes |
| `lifecycle.NewManager()` + `signalx.Run` | yes | yes | no |
| `lifecycle.NewManager()` + `signalx.RunWithForceQuit` | yes | yes | yes |

All four combinations are valid; callers mix and match based on needs.

---

## Trade-offs

### What this spec adds
- **Time-bounded shutdown.** No more indefinite hangs from a stuck service.
- **Operator-friendly force-quit.** Users can double-tap Ctrl+C without
  needing to open another shell.
- **Clean separation.** Mechanism (timeout, force-quit) is decoupled from
  policy (which to enable) via two opt-in APIs.

### What this spec does NOT add
- **Per-service timeout enforcement.** Service authors remain responsible
  for their own `context.WithTimeout` inside `Stop()`. Manager timeout is
  the backstop, not the primary mechanism.
- **No graceful cancellation propagation.** `Stop()` signature stays
  `error`-only; no `context.Context` plumbed through. Services that want
  cancellation must implement it internally.
- **No log of which services were unfinished at timeout.** The warning
  log only states the timeout itself. Attribution of "who was slow" would
  require service-level instrumentation, out of scope here.

### Costs accepted
- **Goroutine leak on timeout.** When Manager timeout fires, the
  background `wg.Wait()` goroutine stays alive until the stuck service
  eventually returns (or the process exits). This is the price of Go's
  "no goroutine kill" rule.
- **`syscall.Kill` is Unix-only.** `syscall.SIGKILL` exists on Windows
  but does not work the same way. signalx is already Unix-oriented
  (`DefaultSignals` references `syscall.SIGTERM`), so this is consistent.
  A Windows-compatible force-quit would need `os.Exit(1)`, deferred until
  someone needs it.
- **Subprocess-based SIGKILL test.** More complex than in-process tests,
  but unavoidable — there's no way to test "process gets killed" without
  a process to kill.

---

## File Structure

No new files. Changes to existing files:

```
go-common/lifecycle/
├── manager.go           # Add: NewManager, Option, WithStopTimeout, stopTimeout field
├── manager_test.go      # Add: tests for new constructor, options, timeout behavior
└── (lifecycle.go unchanged)

go-common/signalx/
├── signalx.go           # Add: RunWithForceQuit
└── signalx_test.go      # Add: tests for RunWithForceQuit incl. subprocess SIGKILL test
```

---

## Documentation Updates Required

After implementation:

1. **`2026-06-11-lifecycle-concurrent-design.md`**:
   - Update Non-Goals section: remove "No graceful-shutdown timeout at the
     Manager level" bullet.
   - Add reference to this spec in a "Superseded by" note.

2. **`2026-06-11-signalx-design.md`**:
   - Update Non-Goals section: remove "no second-signal force-quit" clause.
   - Add reference to this spec for the new `RunWithForceQuit` function.
   - Update Out of Scope (Future): remove the `RunWithForceQuit` bullet
     (it's now in scope).

3. **Obsidian vault**: mirror this spec to
   `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/only/services/go-common/design/v1/`
   per project rules. Wikilink to related lifecycle and signalx notes.

---

## Related

- [[2026-06-11-lifecycle-concurrent-design]] — original Manager design
- [[2026-06-11-signalx-design]] — original signalx design
