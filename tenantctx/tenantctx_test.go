package tenantctx

import (
	"context"
	"testing"

	"google.golang.org/grpc"
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

// TestForwardInterceptorStampsOutgoing pins the portal proxy dial contract
// (deferred from phase ① until first mount): a ctx carrying a trusted tenant
// key must reach the wire with exactly one x-tenant-key metadata value, and a
// ctx without one must not invent the header.
func TestForwardInterceptorStampsOutgoing(t *testing.T) {
	capture := func(t *testing.T, ctx context.Context) metadata.MD {
		t.Helper()
		var got metadata.MD
		invoker := func(c context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			var ok bool
			got, ok = metadata.FromOutgoingContext(c)
			if !ok {
				got = metadata.MD{}
			}
			return nil
		}
		if err := ForwardTenantKeyUnary()(ctx, "/pkg.Svc/Method", nil, nil, nil, invoker); err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		return got
	}

	t.Run("with tenant key", func(t *testing.T) {
		md := capture(t, WithTenantKey(context.Background(), "ten_abc123def456"))
		if got := md.Get(HeaderTenantKey); len(got) != 1 || got[0] != "ten_abc123def456" {
			t.Fatalf("outgoing metadata x-tenant-key = %v, want exactly [ten_abc123def456]", got)
		}
	})

	t.Run("without tenant key", func(t *testing.T) {
		if got := capture(t, context.Background()).Get(HeaderTenantKey); len(got) != 0 {
			t.Fatalf("outgoing metadata must not carry x-tenant-key without a ctx key, got %v", got)
		}
	})

	t.Run("appends to existing outgoing metadata", func(t *testing.T) {
		base := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", "t-1")
		md := capture(t, WithTenantKey(base, "ten_abc123def456"))
		if got := md.Get("x-trace-id"); len(got) != 1 || got[0] != "t-1" {
			t.Fatalf("existing outgoing metadata must be preserved, got %v", got)
		}
		if got := md.Get(HeaderTenantKey); len(got) != 1 {
			t.Fatalf("x-tenant-key must be appended once, got %v", got)
		}
	})
}
