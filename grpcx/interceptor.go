// Package grpcx provides gRPC server utilities and middleware.
package grpcx

import (
	"context"

	"github.com/servekit/go-common/xerr"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// categoryToGRPC maps xerr categories to gRPC status codes.
var categoryToGRPC = map[xerr.Category]codes.Code{
	xerr.CategoryBadRequest:         codes.InvalidArgument,
	xerr.CategoryUnauthorized:       codes.Unauthenticated,
	xerr.CategoryForbidden:          codes.PermissionDenied,
	xerr.CategoryNotFound:           codes.NotFound,
	xerr.CategoryConflict:           codes.AlreadyExists,
	xerr.CategoryTooManyRequests:    codes.ResourceExhausted,
	xerr.CategoryInternal:           codes.Internal,
	xerr.CategoryServiceUnavailable: codes.Unavailable,
}

// ErrorInterceptor translates *xerr.Error to gRPC status errors.
// Non-xerr errors are passed through unchanged.
func ErrorInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err == nil {
		return resp, nil
	}

	xerrErr, ok := err.(*xerr.Error)
	if !ok {
		return nil, err
	}

	grpcCode := categoryToGRPC[xerrErr.Category()]
	return nil, status.Error(grpcCode, xerrErr.Error())
}
