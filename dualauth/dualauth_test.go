package dualauth

import (
	"testing"

	"github.com/servekit/go-common/tenantctx"
	"google.golang.org/grpc/metadata"
)

// TestResolve pins the D-③1 precedence matrix: x-tenant-key is authoritative
// and discards any legacy credentials riding along in the same metadata;
// legacy counts only as a complete ak+sk pair; anything less is unauthenticated.
func TestResolve(t *testing.T) {
	tests := []struct {
		name          string
		md            metadata.MD
		wantTenantKey string
		wantAppKey    string
		wantAppSecret string
		wantSource    Source
	}{
		{
			name: "tenant key wins and legacy creds are discarded",
			md: metadata.Pairs(
				tenantctx.HeaderTenantKey, "ten_abc123def456",
				HeaderAppKey, "ak-legacy",
				HeaderAppSecret, "sk-legacy",
			),
			wantTenantKey: "ten_abc123def456",
			wantSource:    SourceTrusted,
		},
		{
			name:          "tenant key alone",
			md:            metadata.Pairs(tenantctx.HeaderTenantKey, "ten_platform"),
			wantTenantKey: "ten_platform",
			wantSource:    SourceTrusted,
		},
		{
			name:          "full legacy pair without tenant key",
			md:            metadata.Pairs(HeaderAppKey, "ak-1", HeaderAppSecret, "sk-1"),
			wantAppKey:    "ak-1",
			wantAppSecret: "sk-1",
			wantSource:    SourceLegacy,
		},
		{
			name: "half creds (ak without sk) is unauthenticated",
			md:   metadata.Pairs(HeaderAppKey, "ak-1"),
			// legacy validation is per-service; a lone ak never reaches it
			wantSource: SourceNone,
		},
		{
			name:       "half creds (sk without ak) is unauthenticated",
			md:         metadata.Pairs(HeaderAppSecret, "sk-1"),
			wantSource: SourceNone,
		},
		{
			name:       "no credentials at all",
			md:         metadata.Pairs("x-trace-id", "t-1"),
			wantSource: SourceNone,
		},
		{
			name:       "nil metadata",
			md:         nil,
			wantSource: SourceNone,
		},
		{
			name:       "empty tenant key is treated as absent",
			md:         metadata.Pairs(tenantctx.HeaderTenantKey, ""),
			wantSource: SourceNone,
		},
		{
			name: "empty tenant key falls through to legacy",
			md: metadata.Pairs(
				tenantctx.HeaderTenantKey, "",
				HeaderAppKey, "ak-1",
				HeaderAppSecret, "sk-1",
			),
			wantAppKey:    "ak-1",
			wantAppSecret: "sk-1",
			wantSource:    SourceLegacy,
		},
		{
			name: "empty legacy cred value is treated as absent",
			md: metadata.MD{
				"x-app-key":    {""},
				"x-app-secret": {"sk-1"},
			},
			wantSource: SourceNone,
		},
		{
			// maps built by hand (not via Pairs/Append) can carry non-lowercase
			// keys; lookups must stay case-insensitive like tenantctx.firstValue
			name: "case-insensitive keys on hand-built metadata",
			md: metadata.MD{
				"X-Tenant-Key": {"ten_abc123def456"},
				"X-App-Key":    {"ak-1"},
				"X-App-Secret": {"sk-1"},
			},
			wantTenantKey: "ten_abc123def456",
			wantSource:    SourceTrusted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantKey, appKey, appSecret, source := Resolve(tt.md)
			if tenantKey != tt.wantTenantKey {
				t.Errorf("Resolve() tenantKey = %q, want %q", tenantKey, tt.wantTenantKey)
			}
			if appKey != tt.wantAppKey {
				t.Errorf("Resolve() appKey = %q, want %q", appKey, tt.wantAppKey)
			}
			if appSecret != tt.wantAppSecret {
				t.Errorf("Resolve() appSecret = %q, want %q", appSecret, tt.wantAppSecret)
			}
			if source != tt.wantSource {
				t.Errorf("Resolve() source = %v, want %v", source, tt.wantSource)
			}
		})
	}
}
