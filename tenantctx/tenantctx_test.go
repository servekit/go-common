package tenantctx

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestTenantKeyRoundTrip(t *testing.T) {
	ctx := WithTenantKey(context.Background(), "ten_abc123def456")
	got, ok := TenantKeyFromCtx(ctx)
	if !ok || got != "ten_abc123def456" {
		t.Fatalf("want ten_abc123def456 ok=true, got %q ok=%v", got, ok)
	}
	if _, ok := TenantKeyFromCtx(context.Background()); ok {
		t.Fatal("empty ctx must not yield a tenant key")
	}
}

func TestTrustedInterceptorLiftsMetadata(t *testing.T) {
	md := metadata.Pairs(HeaderTenantKey, "ten_abc123def456")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	var seen string
	_, err := TrustedTenantKeyUnary()(ctx, nil, nil, func(c context.Context, _ any) (any, error) {
		seen, _ = TenantKeyFromCtx(c)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if seen != "ten_abc123def456" {
		t.Fatalf("handler saw %q", seen)
	}
}

func TestStripInterceptorRemovesInbound(t *testing.T) {
	md := metadata.Pairs(HeaderTenantKey, "ten_spoofed")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := StripInboundTenantKeyUnary()(ctx, nil, nil, func(c context.Context, _ any) (any, error) {
		if _, ok := TenantKeyFromCtx(c); ok {
			t.Fatal("stripped ctx must not carry a tenant key")
		}
		if md2, ok := metadata.FromIncomingContext(c); ok {
			if len(md2.Get(HeaderTenantKey)) != 0 {
				t.Fatal("metadata must not carry x-tenant-key after strip")
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
}
