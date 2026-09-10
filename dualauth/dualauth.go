// Package dualauth resolves inbound caller metadata during the phase ③
// dual-stack window: trusted x-tenant-key (injected by the portal proxy,
// internal-network trust) takes precedence over legacy
// x-app-key/x-app-secret. The package performs NO verification — trusted
// trust comes from the network invariant, legacy validation stays
// per-service. The whole package is deleted when the window closes (phase ④).
// Rule reference: specs/2026-09-10-tenant-platform-design.md D-③1.
package dualauth

import (
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/servekit/go-common/tenantctx"
)

// HeaderAppKey and HeaderAppSecret are the legacy per-app credential
// headers, validated per-service by existing appauth. Window-only.
const (
	HeaderAppKey    = "x-app-key"
	HeaderAppSecret = "x-app-secret"
)

// Source classifies how Resolve identified the caller on the wire.
type Source int

const (
	// SourceNone means no usable credentials: neither a trusted tenant key
	// nor a complete legacy pair. Callers must treat the request as
	// unauthenticated (fail closed).
	SourceNone Source = iota
	// SourceTrusted means x-tenant-key was present and authoritative; any
	// legacy credentials in the same metadata were discarded.
	SourceTrusted
	// SourceLegacy means only a complete x-app-key/x-app-secret pair was
	// present; validation remains the caller's job.
	SourceLegacy
)

// Resolve applies the D-③1 precedence rule to inbound metadata:
//
//   - non-empty x-tenant-key → (key, "", "", SourceTrusted), legacy
//     credentials ignored even when present (prevents smuggled app creds
//     from impersonating another tenant through the proxy);
//   - otherwise a complete non-empty x-app-key + x-app-secret pair →
//     ("", ak, sk, SourceLegacy);
//   - otherwise ("", "", "", SourceNone).
//
// Empty header values count as absent. Resolve performs no verification of
// any kind and never mutates md.
func Resolve(md metadata.MD) (tenantKey, appKey, appSecret string, source Source) {
	if key := firstValue(md, tenantctx.HeaderTenantKey); key != "" {
		return key, "", "", SourceTrusted
	}
	ak := firstValue(md, HeaderAppKey)
	sk := firstValue(md, HeaderAppSecret)
	if ak != "" && sk != "" {
		return "", ak, sk, SourceLegacy
	}
	return "", "", "", SourceNone
}

// firstValue returns the first value stored under key, tolerating
// hand-built maps whose keys escaped metadata's lowercasing (same pattern
// as tenantctx's lookup). Returns "" when the key is absent or empty.
func firstValue(md metadata.MD, key string) string {
	if v := md.Get(key); len(v) > 0 {
		return v[0]
	}
	for k, v := range md {
		if len(v) > 0 && strings.EqualFold(k, key) {
			return v[0]
		}
	}
	return ""
}
