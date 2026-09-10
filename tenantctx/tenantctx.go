// Package tenantctx carries the trusted tenant identity (x-tenant-key)
// through handler contexts, mirroring grpcx's actor plumbing. Phase
// reference: specs/2026-09-10-tenant-platform-design.md §3, §8.2.
package tenantctx

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// HeaderTenantKey is the single trusted metadata key for the tenant the
// request operates as. Only the two doors (portal proxy, testkit gateway)
// may write it; every other hop reads it.
const HeaderTenantKey = "x-tenant-key"

type tenantKeyType struct{}

// WithTenantKey attaches a trusted tenant key to ctx.
func WithTenantKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, tenantKeyType{}, key)
}

// TenantKeyFromCtx returns the trusted tenant key, or ok=false.
func TenantKeyFromCtx(ctx context.Context) (string, bool) {
	k, ok := ctx.Value(tenantKeyType{}).(string)
	return k, ok && k != ""
}

// TrustedTenantKeyUnary lifts x-tenant-key from incoming metadata into the
// handler context. Performs NO verification: mounting it asserts the
// listener sits inside the trusted network behind the doors.
func TrustedTenantKeyUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := firstValue(md); v != "" {
				ctx = WithTenantKey(ctx, v)
			}
		}
		return handler(ctx, req)
	}
}

// StripInboundTenantKeyUnary is for the doors: it removes any x-tenant-key
// an external caller tried to smuggle in before the request is trusted.
func StripInboundTenantKeyUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if firstValue(md) != "" {
				md = md.Copy()
				md.Delete(HeaderTenantKey)
				ctx = metadata.NewIncomingContext(ctx, md)
			}
		}
		return handler(ctx, req)
	}
}

// ForwardTenantKeyUnary copies the ctx tenant key into outgoing metadata,
// mirroring grpcx.ForwardActorUnary. Install as a default dial option.
func ForwardTenantKeyUnary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if k, ok := TenantKeyFromCtx(ctx); ok {
			ctx = metadata.AppendToOutgoingContext(ctx, HeaderTenantKey, k)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func firstValue(md metadata.MD) string {
	if v := md.Get(HeaderTenantKey); len(v) > 0 {
		return v[0]
	}
	for k, v := range md {
		if len(v) > 0 && strings.EqualFold(k, HeaderTenantKey) {
			return v[0]
		}
	}
	return ""
}
