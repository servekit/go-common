package grpcx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/servekit/go-common/xerr"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
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
		`msg="grpc unary call"`,
		"method=/test.v1.Svc/Method",
		"code=OK",
		"peer=1.2.3.4:5678",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}

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
		`error="rpc error: code = InvalidArgument desc = bad input"`,
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
		`error="rpc error: code = Internal desc = boom"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}

func TestLoggingInterceptor_DeadlineExceeded_LogsWarn(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}
	handlerErr := status.Error(codes.DeadlineExceeded, "too slow")

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
		"code=DeadlineExceeded",
		`error="rpc error: code = DeadlineExceeded desc = too slow"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q\nfull log:\n%s", want, log)
		}
	}
}

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

func TestLoggingInterceptor_XerrTranslatedByErrorInterceptor(t *testing.T) {
	buf, restore := swapLogger(t)
	defer restore()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Method"}

	// xerr.New(...) defines a Code; .New(...) on the Code creates the actual
	// *xerr.Error instance. Error() format is "REASON: message", which then
	// becomes the gRPC status description after ErrorInterceptor translates
	// it.
	xerrErr := xerr.New("TEST_REASON", xerr.CategoryBadRequest, 400, "bad input").New()
	handler := func(_ context.Context, _ any) (any, error) {
		return "ignored", xerrErr
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
	if !strings.Contains(log, `error="rpc error: code = InvalidArgument desc = TEST_REASON: bad input"`) {
		t.Errorf("expected translated gRPC error message, got:\n%s", log)
	}
}
