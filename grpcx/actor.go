// Request-actor plumbing: the verified caller identity (common.v1.RequestActor)
// travels from the HTTP edge into handler contexts and across gRPC service
// boundaries as a single trusted metadata header.

package grpcx

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/servekit/api/gen/go/common/v1"
)

// HeaderActor is the single trusted metadata key carrying the serialized
// RequestActor. The edge middleware (user-service/pkg/auth) is its ONLY
// writer and strips inbound copies first. Adding a field to RequestActor
// needs no new header — the serialized message carries it.
const HeaderActor = "x-actor"

// WireActorHeader is the HTTP form grpc-gateway forwards as metadata.
const WireActorHeader = "Grpc-Metadata-X-Actor"

type actorKeyType struct{}

// WithActor attaches a verified actor to ctx.
func WithActor(ctx context.Context, a *commonv1.RequestActor) context.Context {
	return context.WithValue(ctx, actorKeyType{}, a)
}

// ActorFromCtx returns the verified actor, or ok=false for anonymous calls
// (public routes, internal system calls).
func ActorFromCtx(ctx context.Context) (*commonv1.RequestActor, bool) {
	a, ok := ctx.Value(actorKeyType{}).(*commonv1.RequestActor)
	return a, ok && a.GetUserId() != 0
}

// MustActorFromCtx is for handlers that require an authenticated caller.
// The error is plain; callers map it to their unauthorized code.
func MustActorFromCtx(ctx context.Context) (*commonv1.RequestActor, error) {
	if a, ok := ActorFromCtx(ctx); ok {
		return a, nil
	}
	return nil, errors.New("grpcx: no request actor in context")
}

// MarshalActor encodes an actor for the wire (base64 of the proto bytes).
// Shared by the edge middleware and ForwardActorUnary.
func MarshalActor(a *commonv1.RequestActor) (string, error) {
	b, err := proto.Marshal(a)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// UnmarshalActor decodes a wire value. A zero user id is rejected: actors
// exist only for verified users.
func UnmarshalActor(raw string) (*commonv1.RequestActor, error) {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var a commonv1.RequestActor
	if err := proto.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	if a.GetUserId() == 0 {
		return nil, errors.New("grpcx: actor with zero user id")
	}
	return &a, nil
}

// TrustedActorUnary lifts the trusted x-actor metadata into the handler
// context. It performs NO verification: mounting it asserts the gRPC
// listener sits inside the trusted network behind the edge (the same
// posture as the other internal services). Malformed values fail closed —
// the context stays unannotated and identity-requiring handlers reject.
func TrustedActorUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if raw := firstActorValue(md); raw != "" {
				if a, err := UnmarshalActor(raw); err == nil {
					ctx = WithActor(ctx, a)
				}
			}
		}
		return handler(ctx, req)
	}
}

// ForwardActorUnary copies the ctx actor into outgoing metadata so identity
// crosses service boundaries in gRPC mode. Install as a default dial option
// in each service's pkg/client NewClient. Anonymous contexts forward
// nothing.
func ForwardActorUnary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if a, ok := ActorFromCtx(ctx); ok {
			if raw, err := MarshalActor(a); err == nil {
				ctx = metadata.AppendToOutgoingContext(ctx, HeaderActor, raw)
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// firstActorValue returns the first metadata value for HeaderActor. grpc-gateway
// annotations can leave canonical casing on map keys, so fall back to a
// case-insensitive scan.
func firstActorValue(md metadata.MD) string {
	if values := md.Get(HeaderActor); len(values) > 0 {
		return values[0]
	}
	for k, values := range md {
		if len(values) > 0 && strings.EqualFold(k, HeaderActor) {
			return values[0]
		}
	}
	return ""
}
