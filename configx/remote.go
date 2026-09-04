package configx

// Mode selects how a service reaches a remote dependency: in-process
// (module) or over gRPC. It is a string kind so YAML/env values decode
// directly into it; the zero value is treated as ModeModule at resolve
// time (the historical empty-means-module semantics).
type Mode string

const (
	// ModeUnspecified leaves the choice to the resolver's default
	// (module). Written config should say module/grpc explicitly.
	ModeUnspecified Mode = ""
	ModeModule      Mode = "module"
	ModeGRPC        Mode = "grpc"
)

// String implements fmt.Stringer.
func (m Mode) String() string { return string(m) }

// RemoteServiceConfig is the shared shape for a third_party.<name> config
// section: Mode picks the backend, Target dials in grpc mode, Config builds
// the in-process service in module mode. Services alias this in their own
// pkg/config (`type RemoteServiceConfig[T any] = configx.RemoteServiceConfig[T]`)
// so their public config API keeps its name while sharing one definition.
type RemoteServiceConfig[T any] struct {
	Mode   Mode   // "module" | "grpc" ("" = module)
	Target string // gRPC addr, e.g. "localhost:19091" (grpc mode only)
	Config T      // module-mode config
}
