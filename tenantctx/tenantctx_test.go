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

// TestValidTenantKey pins the canonical tenant_key format (spec §3): the
// strict shape "ten_" + 12 chars of [0-9a-z], plus the two reserved
// literals (ten_platform — the console directory; ten_legacy — the merged
// pre-③ directories). Everything else — legacy app_key literals, wrong
// case, wrong length, prefixes — is malformed and must fail closed at every
// credential-presenting entry (phase ④ window close).
func TestValidTenantKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"canonical", "ten_abc123def456", true},
		{"canonical digits only", "ten_000000000000", true},
		{"reserved platform", "ten_platform", true},
		{"reserved legacy", "ten_legacy", true},
		{"empty", "", false},
		{"bare prefix", "ten_", false},
		{"too short", "ten_abc123def45", false},
		{"too long", "ten_abc123def4567", false},
		{"uppercase", "ten_ABC123DEF456", false},
		{"illegal char", "ten_abc123def45_", false},
		{"legacy app_key literal", "testkit", false},
		{"legacy app_key shape", "usr_bh8oqwhx", false},
		{"wrong prefix", "msg_abc123def456", false},
		{"reserved with suffix", "ten_platform2", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidTenantKey(tt.key); got != tt.want {
				t.Fatalf("ValidTenantKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestWithTenantPlantsIncomingAndOutgoing pins the module-mode mirror of
// what the doors do on the wire: the trusted key lands on BOTH incoming
// metadata (in-process calls resolve it there) and outgoing metadata (a
// real gRPC client forwards it), and pre-existing incoming keys survive
// the plant (merge, not replace).
func TestWithTenantPlantsIncomingAndOutgoing(t *testing.T) {
	base := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-client-info", "ios"))
	ctx := WithTenant(base, "ten_abc123def456")

	in, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("incoming metadata must exist after WithTenant")
	}
	if got := in.Get(HeaderTenantKey); len(got) != 1 || got[0] != "ten_abc123def456" {
		t.Fatalf("incoming x-tenant-key = %v, want [ten_abc123def456]", got)
	}
	if got := in.Get("x-client-info"); len(got) != 1 || got[0] != "ios" {
		t.Fatalf("existing incoming keys must survive, got x-client-info=%v", got)
	}
	out, _ := metadata.FromOutgoingContext(ctx)
	if got := out.Get(HeaderTenantKey); len(got) != 1 || got[0] != "ten_abc123def456" {
		t.Fatalf("outgoing x-tenant-key = %v, want [ten_abc123def456]", got)
	}
}

// TestTrustedKeyFromIncoming pins the service-layer read the tenantres
// packages replaced dualauth.Resolve with: first value wins, a hand-built
// map whose key escaped metadata lowercasing still resolves
// (case-tolerant), and absent/empty means no trusted credential.
func TestTrustedKeyFromIncoming(t *testing.T) {
	t.Run("plain metadata", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(HeaderTenantKey, "ten_abc123def456"))
		got, ok := TrustedKeyFromIncoming(ctx)
		if !ok || got != "ten_abc123def456" {
			t.Fatalf("got %q ok=%v, want ten_abc123def456 ok=true", got, ok)
		}
	})
	t.Run("first value wins", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(HeaderTenantKey, "ten_first0000000", HeaderTenantKey, "ten_second000000"))
		got, _ := TrustedKeyFromIncoming(ctx)
		if got != "ten_first0000000" {
			t.Fatalf("got %q, want the first value", got)
		}
	})
	t.Run("case-tolerant lookup", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{"X-Tenant-Key": []string{"ten_abc123def456"}})
		got, ok := TrustedKeyFromIncoming(ctx)
		if !ok || got != "ten_abc123def456" {
			t.Fatalf("got %q ok=%v, want ten_abc123def456 ok=true", got, ok)
		}
	})
	t.Run("absent", func(t *testing.T) {
		if _, ok := TrustedKeyFromIncoming(context.Background()); ok {
			t.Fatal("ctx without metadata must not yield a trusted key")
		}
	})
	t.Run("empty value counts as absent", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(HeaderTenantKey, ""))
		if _, ok := TrustedKeyFromIncoming(ctx); ok {
			t.Fatal("empty x-tenant-key must count as absent")
		}
	})
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
