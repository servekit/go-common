// Package grpcx provides gRPC server utilities and middleware.
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

// healthMethodPrefix is the gRPC health-check service prefix. Methods under
// this prefix (Check, Watch) are skipped by LoggingInterceptor to avoid
// flooding logs with k8s/LB probe traffic.
const healthMethodPrefix = "/grpc.health.v1.Health/"

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

// isClientError returns true for gRPC codes that indicate a client-side
// mistake (HTTP 4xx analog). Server-side codes (Internal, Unavailable, etc.)
// and OK return false.
//
// The classification mirrors HTTP semantics: client errors are expected
// (caller sent something wrong, deadline too tight, etc.), server errors
// indicate bugs or outages. DeadlineExceeded is classified as client-side
// because a tight client deadline is the common trigger and the resulting
// Error-level noise would otherwise page operators for caller behavior.
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
		codes.Canceled,
		codes.DeadlineExceeded:
		return true
	}
	return false
}
