# grpcx: Unary Logging Interceptor

Date: 2026-06-23

Adds a `LoggingInterceptor` to `go-common/grpcx` that emits one structured
slog entry per unary RPC. Fills the gap that most services currently have
no access log at all — RPCs come and go with zero observability.

## Background

`grpcx.Server` currently ships exactly one interceptor: `ErrorInterceptor`,
which translates `*xerr.Error` to gRPC status codes. Nothing records that
a call happened, how long it took, what status it returned, or who called
it. Services adopting `grpcx.New(...)` get error mapping for free but no
access log.

Each service could write its own logging interceptor, but that duplicates
work and drifts. One canonical interceptor in `grpcx` matches the same
pattern as `ErrorInterceptor` — opinionated, used by simply listing it in
the chain.

## Goal

Add `grpcx.LoggingInterceptor` as a `grpc.UnaryServerInterceptor` that:

- Emits exactly one slog entry per unary RPC, after the handler returns.
- Picks the log level based on outcome (Info / Warn / Error), so services
  that configure slog at Warn or Error automatically filter low-priority
  entries.
- Logs four fields: `method`, `duration_ms`, `code`, `peer`.
- Skips gRPC health-check methods so k8s/LB probes don't flood the log.

## Non-Goals

- **No request/response payload logging.** Out of scope; risks leaking
  PII and inflating log volume. A future opt-in Option could add this if
  needed; not in v1.
- **No streaming interceptor.** YAGNI; no current go-common consumer has
  streaming RPCs. Can be added later without breaking the unary one.
- **No trace ID / request ID propagation.** Would require metadata
  conventions or OpenTelemetry integration. Separate concern.
- **No sampling.** Every call logs. High-traffic services that need
  sampling should lower their slog level or wrap the interceptor.
- **No per-service configuration.** The interceptor is a plain function,
  not a configurable builder. Behavior is fixed; level filtering happens
  via slog's `HandlerOptions.Level`.

## API

```go
// LoggingInterceptor emits one structured slog entry per unary RPC after
// it completes. The log level reflects the outcome (Info on success, Warn
// on client errors, Error on server errors), so the service's configured
// slog level naturally filters what gets emitted.
//
// Place this interceptor first (outermost) in the chain so it observes
// the final status code after downstream interceptors (e.g. ErrorInterceptor,
// protovalidate) have translated or rejected the request:
//
//   grpcx.New(cfg, registerGRPC, registerGW,
//       grpcx.LoggingInterceptor,                              // outermost
//       grpcx.ErrorInterceptor,
//       protovalidate_middleware.UnaryServerInterceptor(v),
//   )
func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error)
```

Same signature shape as `ErrorInterceptor`. No options, no builder.

## Behavior

### Level by outcome

| Outcome | gRPC code | Level |
|---|---|---|
| Success | OK | Info |
| Client error | InvalidArgument, NotFound, AlreadyExists, PermissionDenied, Unauthenticated, ResourceExhausted, FailedPrecondition, OutOfRange, Canceled | Warn |
| Server error | Internal, Unknown, Unavailable, DeadlineExceeded, DataLoss, Aborted, Unimplemented | Error |

The classification mirrors HTTP 4xx-vs-5xx semantics: client errors are
expected (caller sent something wrong), server errors indicate bugs or
outages.

### Fields

| Field | Source |
|---|---|
| `method` | `info.FullMethod` (e.g. `/message.v1.MessageService/SendEmail`) |
| `duration_ms` | `time.Since(start).Milliseconds()` |
| `code` | `status.Code(err).String()` (returns `"OK"` when `err == nil`) |
| `peer` | `peer.FromContext(ctx).Addr.String()`, or omitted when unavailable |

On Warn/Error entries, also include `error` with `err.Error()`.

### Health check skipping

Methods matching the prefix `/grpc.health.v1.Health/` are skipped entirely
— no log entry, no time measurement overhead beyond the prefix check.
Covers both `Check` (called by k8s/LB probes) and the streaming `Watch`
method (future-proofing if a streaming interceptor is ever added).

### Logger

Uses `slog.Default()` via `slog.Info(...)` / `slog.Warn(...)` /
`slog.Error(...)` package-level functions. Services configure the global
logger once at startup via `logging.Setup(cfg)` (already established
convention), so the interceptor picks up that configuration with no
plumbing.

## Chain Ordering

The recommended order in `grpcx.New(...)`:

```go
grpcx.New(cfg, registerGRPC, registerGW,
    grpcx.LoggingInterceptor,                                    // 1: outermost
    grpcx.ErrorInterceptor,                                      // 2: xerr → status
    protovalidate_middleware.UnaryServerInterceptor(validator),   // 3: reject bad req
)
```

`grpc.ChainUnaryInterceptor(A, B, C)` runs `A`'s pre-handler phase first,
then `B`'s, then `C`'s, then the handler; on the way out, `C`'s post-phase
runs first, then `B`'s, then `A`'s. So `LoggingInterceptor` at position 1
wraps everything: it observes whatever final error code bubbles up from
`ErrorInterceptor` (which translated `*xerr.Error` to a gRPC status) or
from protovalidate (which returns `InvalidArgument` directly).

If logging is placed *after* `ErrorInterceptor`, logging would see the
raw `*xerr.Error` instead of the translated gRPC status, and `status.Code()`
would return `Unknown` — losing the category. So order matters.

## Implementation Sketch

```go
// logging.go
package grpcx

import (
    "context"
    "log/slog"
    "strings"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/peer"
    "google.golang.org/grpc/status"
)

const healthMethodPrefix = "/grpc.health.v1.Health/"

// LoggingInterceptor ... (doc comment as in API section above)
func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
    if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
        return handler(ctx, req)
    }

    start := time.Now()
    resp, err := handler(ctx, req)
    durationMs := time.Since(start).Milliseconds()
    code := status.Code(err)

    attrs := []any{
        "method", info.FullMethod,
        "duration_ms", durationMs,
        "code", code.String(),
    }
    if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
        attrs = append(attrs, "peer", p.Addr.String())
    }

    switch {
    case err == nil:
        slog.Info("grpc unary call", attrs...)
    case isClientError(code):
        attrs = append(attrs, "error", err.Error())
        slog.Warn("grpc unary call", attrs...)
    default:
        attrs = append(attrs, "error", err.Error())
        slog.Error("grpc unary call", attrs...)
    }

    return resp, err
}

// isClientError returns true for gRPC codes that indicate a client-side
// mistake (HTTP 4xx analog). Server-side codes (Internal, Unavailable, etc.)
// and OK return false.
func isClientError(code codes.Code) bool {
    switch code {
    case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
        codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted,
        codes.FailedPrecondition, codes.OutOfRange, codes.Canceled:
        return true
    }
    return false
}
```

`LoggingInterceptor` is exported at the top of the file; `isClientError`
and `healthMethodPrefix` go below a `// --- internal helpers ---` separator,
matching the file-layout convention in CLAUDE.md.

## Testing

`logging_test.go` exercises the interceptor via a fake handler:

| Test | What it verifies |
|---|---|
| `TestLoggingInterceptor_Success_LogsInfo` | Handler returns nil → slog record at Info with `code=OK`, `duration_ms>=0`, `method`, `peer` populated |
| `TestLoggingInterceptor_ClientError_LogsWarn` | Handler returns `status.Error(codes.InvalidArgument, ...)` → Warn entry with `error` field |
| `TestLoggingInterceptor_ServerError_LogsError` | Handler returns `status.Error(codes.Internal, ...)` → Error entry with `error` field |
| `TestLoggingInterceptor_XerrTranslatedByErrorInterceptor` | Chain LoggingInterceptor+ErrorInterceptor together; handler returns `*xerr.Error` with `CategoryBadRequest`; LoggingInterceptor sees `code=InvalidArgument` (proves ordering works) |
| `TestLoggingInterceptor_HealthCheck_Skipped` | `info.FullMethod = "/grpc.health.v1.Health/Check"` → no slog entry emitted (handler still called, response still returned) |
| `TestLoggingInterceptor_NilPeer_NoPeerField` | `peer.FromContext` returns `ok=false` → `peer` field absent from attrs (no panic) |

Capturing slog output requires swapping `slog.Default()` for a test
handler. Pattern:

```go
var buf bytes.Buffer
prev := slog.Default()
defer slog.SetDefault(prev)
slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
```

Tests assert on the captured text. The Xerr translation test
additionally imports `grpcx.ErrorInterceptor` and chains manually via
nested handler calls (no real gRPC server needed).

## Integration with message-service

After merging into go-common, `message-service/pkg/server.go` updates its
`grpcx.New(...)` call to include `grpcx.LoggingInterceptor` as the first
interceptor. This is the adoption proof-point and confirms the chain
ordering works end-to-end with a real `*xerr.Error` flowing through.

Other services (user-service, pay-service, etc.) opt in by adding the
same one-liner. No service-side configuration required beyond the
existing `logging.Setup(cfg)` call which they already do.

## Trade-offs

### Why one canonical function (not a configurable builder)

The current `ErrorInterceptor` is a plain function and works fine. Adding
options for level policy, field set, payload logging, sampling, etc.
would expand the surface area for a v1 that doesn't need them. If a real
consumer needs customization, add an `Option` later — premature now.

### Why `slog.Default()` instead of injecting `*slog.Logger`

Consistent with `grpcx/server.go` (which uses `slog.Info` directly) and
with `logging.Setup`'s pattern of configuring the global logger once at
startup. Injecting a logger would force every service to plumb one more
dependency through; using the default keeps the API trivial.

### Why skip health checks by hardcoding the prefix

K8s liveness/readiness probes hit `/grpc.health.v1.Health/Check` every
few seconds — at Info level that floods logs. The prefix is stable and
defined by gRPC's own health proto, so hardcoding is no more brittle
than referencing a constant. A configurable skip-list would be overkill
for one well-known method.

### Costs accepted

- **One log entry per call.** At high QPS this is non-trivial I/O.
  Mitigation: services lower the slog level, or wrap the interceptor
  with sampling, or simply don't list it. The default policy favors
  observability over throughput, which is the right default for
  debugging-oriented services.
- **Prefix check on every unary call.** One `strings.HasPrefix` per RPC
  is negligible compared to the actual handler work.
- **No context attribute extraction.** If a service wants request ID,
  trace ID, user ID in the log line, it has to log those itself in the
  handler or add a future interceptor that injects them. This spec
  deliberately doesn't address that.

## File Structure

```
go-common/grpcx/
├── server.go           # unchanged
├── interceptor.go      # unchanged (ErrorInterceptor)
├── logging.go          # NEW: LoggingInterceptor + isClientError + healthMethodPrefix
└── logging_test.go     # NEW: tests listed above

message-service/
└── pkg/server.go       # MODIFIED: add grpcx.LoggingInterceptor as first interceptor
```

## Documentation Updates Required

After implementation:

1. **Obsidian vault**: mirror this spec to
   `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/only/services/go-common/design/v1/grpc-logging-interceptor.md`
   per project rules. Update `services/go-common/index.md` and
   `services/go-common/changes.md`.
2. **Top-level index**: add the spec to
   `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/only/index.md`
   if not already present.

## Related

- `2026-05-22-error-code-design.md` — defines xerr categories that
  `ErrorInterceptor` (and indirectly this logging interceptor) consumes.
