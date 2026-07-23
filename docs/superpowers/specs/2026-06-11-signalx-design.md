# signalx: Signal-Driven Shutdown

Date: 2026-06-11

## Background

Many long-running services follow the same pattern: start components, block
until the process receives a shutdown signal, then stop. The `lifecycle`
package provides a `Manager` that aggregates many services into one.

What is missing is the middle step: blocking on a signal and then triggering
shutdown. Every service re-implements the same boilerplate — `signal.Notify`
on `SIGINT`/`SIGTERM`, `<-ch`, then call stop. See `grpcx/server.go` for one
such hand-rolled instance.

## Goal

Provide a thin, focused package — `signalx` — that wraps the
"start → block-on-signal → stop" sequence around any type implementing the
`signalx.Service` interface (Start + Stop). signalx defines its own minimal
`Service` interface rather than importing lifecycle, keeping the two packages
decoupled. Any type with `Start() error` and `Stop() error` satisfies the
interface, including `*lifecycle.Manager`.

## Non-Goals

- Not a general signal-handling utility (no `SIGHUP`-reload helpers, no signal
  multiplexing, no per-signal callbacks).
- No logging, no `os.Exit`. Caller decides policy. **Note:** second-signal
  force-quit was added in `2026-06-12-shutdown-reliability-design.md` via
  `RunWithForceQuit` — `Run` itself continues to honor the original promise.
- Does not expose a low-level `Wait` primitive — callers needing custom
  sequencing use stdlib `signal.NotifyContext` directly.

## Interface

```go
package signalx

import (
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
```

`*lifecycle.Manager` satisfies `signalx.Service` implicitly (it has
`Start() error` and `Stop() error`), so the common call is `signalx.Run(m)`.
Similarly, `*grpcx.Server` satisfies the interface and can be passed directly.

## Trade-offs

| Decision | Choice | Reason |
|---|---|---|
| Dependency on `lifecycle` | no — signalx defines its own `Service` interface | Decouples the two packages; any type with Start/Stop satisfies the interface |
| Logging | none | Project rule (CLAUDE.md): "基础库原则上不写日志" |
| `os.Exit` | never | Process-exit policy belongs to the caller |
| Second signal (force-quit) | not handled | YAGNI; caller can add `signal.Notify` in parallel if needed |
| Default signal set | `SIGINT`, `SIGTERM` | Conventional Unix graceful-shutdown pair |
| Variadic `sigs` | empty → defaults | Lets callers override (e.g. add `SIGHUP`) without a separate constructor |
| Low-level `Wait` primitive | not exported | YAGNI; stdlib `signal.NotifyContext` covers the escape hatch |

## Usage

A service using `lifecycle` + `signalx` end-to-end:

```go
m := &lifecycle.Manager{}
m.Add("grpc", grpcService)
m.Add("worker", workerService)

if err := signalx.Run(m); err != nil {
    slog.Error("shutdown error", "error", err)
    os.Exit(1)
}
```

A standalone gRPC service using `grpcx.Server` directly:

```go
srv := grpcx.New(cfg, registerGRPC, registerGW)
if err := srv.Run(); err != nil {  // internally calls signalx.Run(s)
    slog.Error("shutdown error", "error", err)
    os.Exit(1)
}
```

## Error Propagation

`Run` returns exactly what `s.Stop()` returns. With `*lifecycle.Manager`,
that is the `errors.Join` of every Service's `Stop` errors (each wrapped with
the component name). The caller decides whether to log, exit, or ignore.

If `s.Start()` returns a non-nil error, `Run` panics with a formatted message.
If `s.Start()` panics, the panic propagates through `Run` unchanged. `Run`
does not trap or wrap panics.

## Testing

| Test | What it verifies |
|---|---|
| `Run_StartsService_BeforeBlocking` | A `fakeService` whose `Start` signals a WaitGroup — `Run` (in a goroutine) triggers it before blocking |
| `Run_StopCalled_AfterSignal` | Send `SIGTERM` via `syscall.Kill(syscall.Getpid(), syscall.SIGTERM)`; verify `Stop` was invoked and `Run` returned |
| `Run_ReturnsStopError` | `Stop` returns a non-nil error → `Run` returns that same error |
| `Run_DefaultSignals_WhenEmpty` | Call `Run(s)` with no sigs; send `SIGTERM`; verify `Stop` is invoked (defaults kicked in) |
| `Run_CustomSignals` | Pass `syscall.SIGHUP`; send `SIGHUP`; verify `Stop` is invoked. Send `SIGTERM` first to confirm it does *not* trigger |
| `Run_StartPanic_Propagates` | `Start` panics → `Run` panics with the same value |
| `Run_StartError_PanicsWithWrap` | `Start` returns a non-nil error → `Run` panics with `fmt.Sprintf("signalx: start failed: %v", err)` |
| `Run_AcceptsManager` | Construct a `*lifecycle.Manager` with one service, pass it to `Run`, send `SIGTERM`, verify `Stop` was invoked and `Run` returned. Proves that `*Manager` satisfies `signalx.Service`. |
| `DefaultSignals_Value` | `DefaultSignals` equals `[]os.Signal{SIGINT, SIGTERM}` — guards against accidental drift |

The signal-sending tests use `syscall.Kill(syscall.Getpid(), sig)` so they
exercise the real kernel path. Each test sends signals to its own process; to
avoid cross-test interference, signal tests must not run in parallel with each
other.

## Directory Layout

```
go-common/
├── signalx/
│   ├── signalx.go       # Service interface, Run, DefaultSignals
│   └── signalx_test.go
└── ...
```

## Out of Scope (Future)

- ~~A second-signal force-quit helper (e.g. `signalx.RunWithForceQuit`~~ —
  **Implemented** in `2026-06-12-shutdown-reliability-design.md`. `signalx.Run`
  still does not force-quit; callers opt in via `RunWithForceQuit`.
- grpcx.Server already integrates with signalx via `Run()` calling
  `signalx.Run(s)`.

## Related

- `2026-06-11-lifecycle-concurrent-design.md` — defines the `Manager` whose
  `Start()/Stop()` implicitly satisfies `signalx.Service`.
