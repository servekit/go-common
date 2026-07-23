# grpcx: Logging Interceptor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `grpcx.LoggingInterceptor` (one slog entry per unary RPC, level by outcome, skips health checks) to `go-common/grpcx`, then adopt it in `message-service/pkg/server.go` as the outermost interceptor in the chain.

**Architecture:** Plain `grpc.UnaryServerInterceptor` function — same shape as `grpcx.ErrorInterceptor`. Uses `slog.Default()` so it picks up the service's globally-configured logger. Must be placed first in the chain so it observes the final status code after `ErrorInterceptor` translates `*xerr.Error` and after protovalidate rejects bad requests. Implementation lives in a new file `grpcx/logging.go` to keep `interceptor.go` focused on `ErrorInterceptor`.

**Tech Stack:** Go stdlib (`context`, `log/slog`, `strings`, `time`, `bytes`, `net`), `google.golang.org/grpc` (interceptor types, `status`, `peer`, `codes`), `code.byteflowing.com/base/go-common/xerr` (for composition test only). No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-06-23-grpc-logging-interceptor-design.md`

---

## File Structure

**New files (in `go-common`):**

| File | Responsibility |
|---|---|
| `grpcx/logging.go` | `LoggingInterceptor` function + `isClientError` helper + `healthMethodPrefix` constant |
| `grpcx/logging_test.go` | Six tests covering success, client/server error levels, nil peer, health check skip, chain composition |

**Modified files:**

| File | Responsibility |
|---|---|
| `message-service/pkg/server.go` | Insert `grpcx.LoggingInterceptor` as first entry in the `grpcx.New(...)` interceptor list |
| `message-service/go.mod` | Bump `code.byteflowing.com/base/go-common` to the commit containing `LoggingInterceptor` |

**Not modified:**
- `grpcx/interceptor.go` — `ErrorInterceptor` is unchanged.
- `grpcx/server.go` — server bootstrap is unchanged; callers opt in by listing the interceptor.

---

## Conventions for the implementing engineer

- **Code comments in English; commit messages in English following Conventional Commits** (`feat(grpcx): ...`, `test(grpcx): ...`, etc.) — see repo root `CLAUDE.md`.
- **File layout convention**: exported `LoggingInterceptor` at the top of `logging.go`; `isClientError` and `healthMethodPrefix` below a `// --- internal helpers ---` separator.
- **No `//nolint` comments** — handle every error explicitly.
- **Library code does not log directly** — except this interceptor, where logging *is* the feature.
- **Run `gofmt -w` and `goimports -w` on every Go file you write.** Then run `golangci-lint run ./...` before committing.
- **All work happens in `go-common` first** (Tasks 1-5), then `message-service` (Task 6). The two repos are at `/Users/moss/code/base/go-common` and `/Users/moss/code/base/message-service`.
- **Each task ends with a commit.** Do not skip commits thinking you'll batch them — the brainstorming/writing-plans workflow expects one commit per task.

---

## Task 1: LoggingInterceptor skeleton + success path

The first test drives the basic shape: handler is called, log entry is emitted at Info with the four fields, response and error pass through. Error-level classification is intentionally deferred to Task 2 — Task 1's version logs at Info unconditionally.

**Files:**
- Create: `/Users/moss/code/base/go-common/grpcx/logging.go`
- Create: `/Users/moss/code/base/go-common/grpcx/logging_test.go`

- [ ] **Step 1: Write the failing test**

Create `grpcx/logging_test.go` with:

```go
package grpcx

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// swapLogger directs slog.Default() output to a buffer for the duration of
// the test, returning the buffer and a restore func. The handler is set to
// LevelDebug so all entries are emitted regardless of level policy.
func swapLogger(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	restore := func() { slog.SetDefault(prev) }
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return &buf, restore
}

func TestLoggingInterceptor_Success_LogsInfo(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5678},
	})
	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}

	var handlerCalled bool
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "resp", nil
	}

	resp, err := LoggingInterceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "resp" {
		t.Fatalf("unexpected resp: %v", resp)
	}
	if !handlerCalled {
		t.Fatal("handler not called")
	}

	log := buf.String()
	for _, want := range []string{
		"level=INFO",
		"msg=grpc unary call",
		"method=/test.v1.Svc/Method",
		"code=OK",
		"peer=1.2.3.4:5678",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run TestLoggingInterceptor_Success_LogsInfo -v
```

Expected: compile error — `LoggingInterceptor` undefined.

- [ ] **Step 3: Write the minimal implementation**

Create `grpcx/logging.go` with:

```go
// Package grpcx provides gRPC server utilities and middleware.
package grpcx

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// LoggingInterceptor emits one structured slog entry per unary RPC after it
// completes. Place this interceptor first (outermost) in the chain so it
// observes the final status code after downstream interceptors (e.g.
// ErrorInterceptor, protovalidate) have translated or rejected the request.
//
// The log level reflects the outcome (Info on success, Warn on client
// errors, Error on server errors), so the service's configured slog level
// naturally filters what gets emitted.
//
// Usage:
//
//	grpcx.New(cfg, registerGRPC, registerGW,
//	    grpcx.LoggingInterceptor,                              // outermost
//	    grpcx.ErrorInterceptor,
//	    protovalidate_middleware.UnaryServerInterceptor(v),
//	)
func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	durationMs := time.Since(start).Milliseconds()
	code := status.Code(err)

	attrs := []any{
		"method", info.FullMethod,
		"duration_ms", durationMs,
		"code", code.String(),
	}
	if p, ok := peer.FromContext(ctx); ok {
		attrs = append(attrs, "peer", p.Addr.String())
	}

	slog.Info("grpc unary call", attrs...)
	return resp, err
}
```

Note: this minimal version logs at Info unconditionally. Task 2 adds the
Info/Warn/Error classification driven by failing tests.

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run TestLoggingInterceptor_Success_LogsInfo -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/moss/code/base/go-common
gofmt -w grpcx/logging.go grpcx/logging_test.go
goimports -w grpcx/logging.go grpcx/logging_test.go
git add grpcx/logging.go grpcx/logging_test.go
git commit -m "feat(grpcx): add LoggingInterceptor skeleton with success path"
```

---

## Task 2: Error-level classification (Warn for client errors, Error for server errors)

Drive out the level policy with two failing tests. The minimal version from Task 1 logs everything at Info; this task adds the switch and the `isClientError` helper.

**Files:**
- Modify: `/Users/moss/code/base/go-common/grpcx/logging.go`
- Modify: `/Users/moss/code/base/go-common/grpcx/logging_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `grpcx/logging_test.go` (add `"google.golang.org/grpc/codes"` and `"google.golang.org/grpc/status"` to imports if not already present):

```go
func TestLoggingInterceptor_ClientError_LogsWarn(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}
	handlerErr := status.Error(codes.InvalidArgument, "bad input")

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, handlerErr
	}

	_, err := LoggingInterceptor(context.Background(), nil, info, handler)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error to pass through, got %v", err)
	}

	log := buf.String()
	for _, want := range []string{
		"level=WARN",
		"code=InvalidArgument",
		"error=rpc error: code = InvalidArgument desc = bad input",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}

func TestLoggingInterceptor_ServerError_LogsError(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}
	handlerErr := status.Error(codes.Internal, "boom")

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, handlerErr
	}

	_, err := LoggingInterceptor(context.Background(), nil, info, handler)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error to pass through, got %v", err)
	}

	log := buf.String()
	for _, want := range []string{
		"level=ERROR",
		"code=Internal",
		"error=rpc error: code = Internal desc = boom",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}
```

Add `"errors"` to the test file's imports as well.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run 'TestLoggingInterceptor_(Client|Server)Error' -v
```

Expected: both FAIL — log contains `level=INFO` instead of `level=WARN` / `level=ERROR`, and the `error=` field is missing.

- [ ] **Step 3: Add classification logic**

Edit `grpcx/logging.go`. Replace the single `slog.Info(...)` call with a switch, and add the `isClientError` helper below a separator.

The body of `LoggingInterceptor` from `start := time.Now()` through `return resp, err` becomes:

```go
	start := time.Now()
	resp, err := handler(ctx, req)
	durationMs := time.Since(start).Milliseconds()
	code := status.Code(err)

	attrs := []any{
		"method", info.FullMethod,
		"duration_ms", durationMs,
		"code", code.String(),
	}
	if p, ok := peer.FromContext(ctx); ok {
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
```

Add `"google.golang.org/grpc/codes"` to imports. Then append after the closing brace of `LoggingInterceptor`:

```go
// --- internal helpers ---

// healthMethodPrefix is the gRPC health-check service prefix. Methods under
// this prefix (Check, Watch) are skipped by LoggingInterceptor to avoid
// flooding logs with k8s/LB probe traffic.
const healthMethodPrefix = "/grpc.health.v1.Health/"

// isClientError returns true for gRPC codes that indicate a client-side
// mistake (HTTP 4xx analog). Server-side codes (Internal, Unavailable, etc.)
// and OK return false.
//
// The classification mirrors HTTP semantics: client errors are expected
// (caller sent something wrong), server errors indicate bugs or outages.
func isClientError(code codes.Code) bool {
	switch code {
	case codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.ResourceExhausted,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Canceled:
		return true
	}
	return false
}
```

`healthMethodPrefix` is defined here so it lives next to the only function
that uses it; it's exercised by Task 4.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -v
```

Expected: all three tests so far (Success, ClientError, ServerError) PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/moss/code/base/go-common
gofmt -w grpcx/logging.go grpcx/logging_test.go
goimports -w grpcx/logging.go grpcx/logging_test.go
git add grpcx/logging.go grpcx/logging_test.go
git commit -m "feat(grpcx): classify RPC errors into Warn (client) / Error (server)"
```

---

## Task 3: Nil-peer regression test

The Task 1 implementation uses `if p, ok := peer.FromContext(ctx); ok`, which is naturally nil-safe: when there is no peer in context (e.g. unit tests that call the interceptor directly), the `peer` field is simply omitted. This task locks in that behavior so a future refactor doesn't accidentally introduce a nil dereference.

**Files:**
- Modify: `/Users/moss/code/base/go-common/grpcx/logging_test.go`

- [ ] **Step 1: Write the test**

Append to `grpcx/logging_test.go`:

```go
func TestLoggingInterceptor_NilPeer_NoPeerField(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}
	handler := func(ctx context.Context, req any) (any, error) {
		return "resp", nil
	}

	// Plain context.Background() — no peer set.
	_, err := LoggingInterceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, "code=OK") {
		t.Errorf("expected code=OK in log, got:\n%s", log)
	}
	if strings.Contains(log, "peer=") {
		t.Errorf("expected no peer field when peer is absent, got:\n%s", log)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run TestLoggingInterceptor_NilPeer_NoPeerField -v
```

Expected: PASS. If it FAILS with a nil pointer panic, the implementation in Task 1 was modified incorrectly — restore the `if p, ok := peer.FromContext(ctx); ok` guard.

- [ ] **Step 3: Commit**

```bash
cd /Users/moss/code/base/go-common
gofmt -w grpcx/logging_test.go
goimports -w grpcx/logging_test.go
git add grpcx/logging_test.go
git commit -m "test(grpcx): lock in nil-peer safety of LoggingInterceptor"
```

---

## Task 4: Health-check skipping

Drive out the prefix-skip behavior. Methods matching `/grpc.health.v1.Health/` (Check, Watch) must not emit a log entry, but the handler must still be called and its response returned.

**Files:**
- Modify: `/Users/moss/code/base/go-common/grpcx/logging.go`
- Modify: `/Users/moss/code/base/go-common/grpcx/logging_test.go`

- [ ] **Step 1: Write the failing test**

Append to `grpcx/logging_test.go`:

```go
func TestLoggingInterceptor_HealthCheck_Skipped(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}

	var handlerCalled bool
	var handlerReq any = "health-req"
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		if req != handlerReq {
			t.Errorf("handler received unexpected req: %v", req)
		}
		return "health-resp", nil
	}

	resp, err := LoggingInterceptor(context.Background(), handlerReq, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "health-resp" {
		t.Fatalf("expected handler response to pass through, got %v", resp)
	}
	if !handlerCalled {
		t.Fatal("handler must still be called for health checks")
	}
	if buf.Len() > 0 {
		t.Errorf("expected no log entry for health check, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run TestLoggingInterceptor_HealthCheck_Skipped -v
```

Expected: FAIL — `buf.Len() > 0` because the current implementation logs every call at Info.

- [ ] **Step 3: Add the prefix check**

Edit `grpcx/logging.go`. Add `"strings"` to imports. At the very top of `LoggingInterceptor`'s body, before `start := time.Now()`, add:

```go
	if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
		return handler(ctx, req)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -v
```

Expected: all five tests so far PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/moss/code/base/go-common
gofmt -w grpcx/logging.go grpcx/logging_test.go
goimports -w grpcx/logging.go grpcx/logging_test.go
git add grpcx/logging.go grpcx/logging_test.go
git commit -m "feat(grpcx): skip health-check methods in LoggingInterceptor"
```

---

## Task 5: Chain composition with ErrorInterceptor

Verify that when `LoggingInterceptor` wraps `ErrorInterceptor`, an `*xerr.Error` returned by the handler is translated to a gRPC status *before* logging sees it — so the log entry shows the correct code (e.g. `InvalidArgument` for `CategoryBadRequest`) instead of `Unknown`. This is the ordering guarantee from the spec, locked in by test.

No new production code in this task; the test purely verifies composition.

**Files:**
- Modify: `/Users/moss/code/base/go-common/grpcx/logging_test.go`

- [ ] **Step 1: Write the test**

Add `"code.byteflowing.com/base/go-common/xerr"` to imports, then append to `grpcx/logging_test.go`:

```go
func TestLoggingInterceptor_XerrTranslatedByErrorInterceptor(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}

	xerrErr := xerr.New("test-reason", xerr.CategoryBadRequest, 400, "bad input")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, xerrErr
	}

	// Manually chain LoggingInterceptor(ErrorInterceptor(handler)). This
	// mirrors the recommended order in grpcx.New(...). The point: by the
	// time LoggingInterceptor's post-handler logging runs, the error has
	// been translated from *xerr.Error to a gRPC status, so status.Code
	// returns InvalidArgument (not Unknown).
	chainedHandler := func(ctx context.Context, req any) (any, error) {
		return ErrorInterceptor(ctx, req, info, handler)
	}

	_, err := LoggingInterceptor(context.Background(), nil, info, chainedHandler)
	if err == nil {
		t.Fatal("expected error from chained interceptors")
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") {
		t.Errorf("expected WARN for client-side xerr, got:\n%s", log)
	}
	if !strings.Contains(log, "code=InvalidArgument") {
		t.Errorf("expected code=InvalidArgument (translated from xerr.CategoryBadRequest), got:\n%s", log)
	}
	if !strings.Contains(log, "error=rpc error: code = InvalidArgument desc = bad input") {
		t.Errorf("expected translated gRPC error message, got:\n%s", log)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

```bash
cd /Users/moss/code/base/go-common && go test ./grpcx/ -run TestLoggingInterceptor_XerrTranslatedByErrorInterceptor -v
```

Expected: PASS. If it FAILS with `code=Unknown`, the interceptors are in the wrong order in the test setup — confirm `chainedHandler` calls `ErrorInterceptor` *inside* `LoggingInterceptor` (as written above).

- [ ] **Step 3: Commit**

```bash
cd /Users/moss/code/base/go-common
gofmt -w grpcx/logging_test.go
goimports -w grpcx/logging_test.go
git add grpcx/logging_test.go
git commit -m "test(grpcx): verify LoggingInterceptor observes ErrorInterceptor-translated status"
```

---

## Task 6: Adopt LoggingInterceptor in message-service

Wire `grpcx.LoggingInterceptor` into `message-service/pkg/server.go` as the first entry in the interceptor list. Bump the go-common dependency to the commit that contains `LoggingInterceptor`. Run the full message-service test suite to confirm nothing regresses.

**Files:**
- Modify: `/Users/moss/code/base/message-service/pkg/server.go`
- Modify: `/Users/moss/code/base/message-service/go.mod`

- [ ] **Step 1: Confirm go-common has the new code on disk**

```bash
cd /Users/moss/code/base/go-common && git log --oneline -5
```

Confirm the last 4-5 commits include the four `feat(grpcx)` / `test(grpcx)` commits from Tasks 1-5. Record the SHA of the most recent of those commits — you'll need it for the go.mod bump below.

- [ ] **Step 2: Inspect the current `grpcx.New(...)` call**

```bash
cd /Users/moss/code/base/message-service && sed -n '60,80p' pkg/server.go
```

You should see the existing `grpcSrv := grpcx.New(...)` block with `grpcx.ErrorInterceptor` and the protovalidate interceptor listed.

- [ ] **Step 3: Bump go-common in go.mod**

If message-service uses a `replace` directive pointing at the local path:

```bash
cd /Users/moss/code/base/message-service && grep -n "go-common" go.mod
```

If there's a `replace code.byteflowing.com/base/go-common => /Users/moss/code/base/go-common` line, nothing to change — the local checkout is picked up automatically. Skip to Step 4.

If go-common is referenced by version/SHA:

```bash
cd /Users/moss/code/base/message-service
go get code.byteflowing.com/base/go-common@<SHA-from-step-1>
go mod tidy
```

- [ ] **Step 4: Wire LoggingInterceptor into `pkg/server.go`**

In `pkg/server.go`, find the `grpcSrv := grpcx.New(...)` call. Insert `grpcx.LoggingInterceptor,` as the **first** interceptor argument, before `grpcx.ErrorInterceptor`. The resulting call should look like:

```go
	grpcSrv := grpcx.New(
		&grpcx.ServerConfig{
			GRPCAddr:    cfg.Server.GRPCAddr,
			GatewayAddr: cfg.Server.HTTPAddr,
		},
		func(s *grpc.Server) { pb.RegisterMessageServiceServer(s, hdl) },
		pb.RegisterMessageServiceHandlerFromEndpoint,
		grpcx.LoggingInterceptor,
		grpcx.ErrorInterceptor,
		protovalidate_middleware.UnaryServerInterceptor(validator),
	)
```

Order matters: `LoggingInterceptor` must be first (outermost) so it observes the status code that `ErrorInterceptor` produces, not the raw `*xerr.Error`.

- [ ] **Step 5: Build and run the full test suite**

```bash
cd /Users/moss/code/base/message-service
go build ./...
go test -race -coverprofile=coverage.out ./...
```

Expected: build succeeds; all tests pass. No coverage regression is expected from a one-line interceptor addition.

- [ ] **Step 6: Smoke-test manually (optional but recommended)**

If the service can be started locally (it needs Redis, Postgres, vendor creds):

```bash
cd /Users/moss/code/base/message-service && go run ./cmd/server/
```

In another terminal, fire an invalid request via grpcurl or curl against the gateway:

```bash
curl -X POST http://localhost:8080/v1/messages/sms/send \
  -H 'Content-Type: application/json' \
  -d '{"invalid": "payload"}'
```

Expected: server logs a line like `level=WARN msg="grpc unary call" method=/message.v1.MessageService/SendSMS code=InvalidArgument ...`. Ctrl+C the server when done.

If local deps aren't available, skip this step — the unit tests cover the same path.

- [ ] **Step 7: Commit**

```bash
cd /Users/moss/code/base/message-service
gofmt -w pkg/server.go
goimports -w pkg/server.go
git add pkg/server.go go.mod go.sum
git commit -m "feat(server): adopt grpcx.LoggingInterceptor as outermost unary interceptor"
```

---

## Done criteria

- [ ] `go-common/grpcx/logging.go` and `grpcx/logging_test.go` exist with 6 passing tests.
- [ ] `golangci-lint run ./...` is clean in `go-common`.
- [ ] `message-service/pkg/server.go` lists `grpcx.LoggingInterceptor` first in the chain.
- [ ] `message-service` builds and all tests pass.
- [ ] Six commits across the two repos (5 in go-common, 1 in message-service).
- [ ] Spec file `docs/superpowers/specs/2026-06-23-grpc-logging-interceptor-design.md` reflects what was built (update only if implementation diverged — it shouldn't).
