// Package grpcx provides gRPC server utilities and middleware.
package grpcx

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"
)

// BearerTokenFromCtx extracts a bearer token from gRPC metadata.
// Works with both direct gRPC calls and grpc-gateway (HTTP Authorization header
// is automatically converted to gRPC metadata by the gateway).
func BearerTokenFromCtx(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("no metadata found")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", fmt.Errorf("authorization header missing")
	}
	parts := strings.SplitN(values[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", fmt.Errorf("invalid authorization header format")
	}
	return parts[1], nil
}
