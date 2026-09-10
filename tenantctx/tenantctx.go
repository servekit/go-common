// Package tenantctx carries the trusted tenant identity (x-tenant-key)
// through handler contexts, mirroring grpcx's actor plumbing. Since the
// phase ④ window close this is the ONLY caller-credential stack: keys are
// planted on the wire solely by the two doors (portal proxy, testkit
// gateway), and consumer services read and format-validate them through
// this package before resolving a tenant. Phase reference:
// specs/2026-09-10-tenant-platform-design.md §3, §8.2.
package tenantctx

import (
	"context"
	"regexp"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// HeaderTenantKey is the single trusted metadata key for the tenant the
// request operates as. Only the two doors (portal proxy, testkit gateway)
// may write it; every other hop reads it.
const HeaderTenantKey = "x-tenant-key"

// The reserved out-of-format literals the platform keeps alongside the
// canonical shape: ten_platform is the console directory every operator
// signs into; ten_legacy is the merged directory the pre-③ legacy aliases
// were mapped into (F1).
const (
	reservedPlatform = "ten_platform"
	reservedLegacy   = "ten_legacy"
)

// tenantKeyPattern is the canonical tenant_key format (spec §3): "ten_" +
// 12 chars of [0-9a-z].
var tenantKeyPattern = regexp.MustCompile(`^ten_[0-9a-z]{12}$`)

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

// ValidTenantKey reports whether key is a well-formed tenant_key (spec §3):
// the canonical shape "ten_" + 12 chars of [0-9a-z], or one of the two
// reserved literals (ten_platform, ten_legacy). Credential-presenting
// entries reject anything else as unauthenticated — a malformed key never
// reaches the directory lookup or the lazy-create path.
func ValidTenantKey(key string) bool {
	if key == reservedPlatform || key == reservedLegacy {
		return true
	}
	return tenantKeyPattern.MatchString(key)
}

// WithTenant plants the trusted tenant key on ctx as BOTH incoming and
// outgoing metadata — the module-mode mirror of what the doors do on the
// wire (HeaderTenantKey). Production callers never plant the key
// themselves (the doors' network position is the trust basis); this exists
// for in-process/module-mode callers and tests. Merge semantics: existing
// incoming keys (client-info et al.) survive the plant.
func WithTenant(ctx context.Context, tenantKey string) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		md = md.Copy()
		md.Set(HeaderTenantKey, tenantKey)
		ctx = metadata.NewIncomingContext(ctx, md)
	} else {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(HeaderTenantKey, tenantKey))
	}
	return metadata.AppendToOutgoingContext(ctx, HeaderTenantKey, tenantKey)
}

// TrustedKeyFromIncoming reads the trusted tenant key from INCOMING
// metadata — the service-layer read that replaced the dualauth
// classification when the window closed. Returns the key verbatim
// (validation is the caller's job, via ValidTenantKey); ok=false when the
// header is absent or empty. Case-tolerant, like firstValue, to survive
// hand-built metadata maps.
func TrustedKeyFromIncoming(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	if key := firstValue(md); key != "" {
		return key, true
	}
	return "", false
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
