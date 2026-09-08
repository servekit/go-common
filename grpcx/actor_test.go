package grpcx

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/stretchr/testify/require"

	commonv1 "github.com/servekit/api/gen/go/common/v1"
)

func TestActorCtxRoundTrip(t *testing.T) {
	ctx := context.Background()

	_, ok := ActorFromCtx(ctx)
	require.False(t, ok, "bare context is anonymous")

	_, err := MustActorFromCtx(ctx)
	require.Error(t, err)

	a := &commonv1.RequestActor{UserId: 42, SessionId: "sid", LoginTarget: "u@x.io", LoginMethod: 2}
	ctx = WithActor(ctx, a)
	got, ok := ActorFromCtx(ctx)
	require.True(t, ok)
	require.Equal(t, int64(42), got.GetUserId())
	require.Equal(t, "sid", got.GetSessionId())

	// A zero user id is not an actor.
	ctx2 := WithActor(ctx, &commonv1.RequestActor{})
	_, ok = ActorFromCtx(ctx2)
	require.False(t, ok)
}

func TestActorWireRoundTrip(t *testing.T) {
	a := &commonv1.RequestActor{UserId: 7, SessionId: "s", LoginTarget: "bob", LoginMethod: 1}
	raw, err := MarshalActor(a)
	require.NoError(t, err)

	got, err := UnmarshalActor(raw)
	require.NoError(t, err)
	require.Equal(t, a.GetUserId(), got.GetUserId())
	require.Equal(t, a.GetLoginMethod(), got.GetLoginMethod())
}

func TestUnmarshalActorFailClosed(t *testing.T) {
	cases := map[string]string{
		"bad base64":     "!!!not-base64!!!",
		"bad proto":      "YWJj", // "abc" — valid base64, invalid proto
		"zero user id":   func() string { r, _ := MarshalActor(&commonv1.RequestActor{}); return r }(),
		"empty string":   "",
		"garbage varint": "gAMDgICAgICAgIj/gICAgICA", // decodes, unmarshal may fail or succeed
	}
	for name, raw := range cases {
		if raw == "" {
			continue // absence is handled by firstActorValue, not unmarshalActor
		}
		if a, err := UnmarshalActor(raw); err == nil {
			require.Equal(t, int64(0), a.GetUserId(), "%s: only a zero-id actor could parse", name)
		}
	}
}

func TestTrustedActorUnary(t *testing.T) {
	icept := TrustedActorUnary()

	a := &commonv1.RequestActor{UserId: 9, SessionId: "sid"}
	raw, err := MarshalActor(a)
	require.NoError(t, err)

	// Metadata with canonical-cased keys (grpc-gateway leaves them) resolves
	// case-insensitively; a canonical actor reaches the handler.
	md := metadata.Pairs("X-Actor", raw)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	var handlerGot *commonv1.RequestActor
	_, err = icept(ctx, nil, &grpc.UnaryServerInfo{}, func(c context.Context, _ any) (any, error) {
		handlerGot, _ = ActorFromCtx(c)
		return nil, nil
	})
	require.NoError(t, err)
	require.NotNil(t, handlerGot)
	require.Equal(t, int64(9), handlerGot.GetUserId())

	// Malformed values fail closed: no actor in the handler context.
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(HeaderActor, "garbage"))
	handlerGot = nil
	_, err = icept(ctx, nil, &grpc.UnaryServerInfo{}, func(c context.Context, _ any) (any, error) {
		handlerGot, _ = ActorFromCtx(c)
		return nil, nil
	})
	require.NoError(t, err)
	require.Nil(t, handlerGot)

	// No metadata: anonymous passthrough.
	handlerGot = nil
	_, err = icept(context.Background(), nil, &grpc.UnaryServerInfo{}, func(c context.Context, _ any) (any, error) {
		handlerGot, _ = ActorFromCtx(c)
		return nil, nil
	})
	require.NoError(t, err)
	require.Nil(t, handlerGot)
}

func TestForwardActorUnaryAppendsMetadata(t *testing.T) {
	// invoker captures the outgoing metadata the interceptor produced.
	var seen metadata.MD
	invoker := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		seen, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}
	icept := ForwardActorUnary()

	a := &commonv1.RequestActor{UserId: 5, SessionId: "sid"}
	require.NoError(t, icept(WithActor(context.Background(), a), "/svc/M", nil, nil, nil, invoker))
	vals := seen.Get(HeaderActor)
	require.Len(t, vals, 1)
	back, err := UnmarshalActor(vals[0])
	require.NoError(t, err)
	require.Equal(t, int64(5), back.GetUserId())

	// Anonymous context forwards nothing.
	seen = nil
	require.NoError(t, icept(context.Background(), "/svc/M", nil, nil, nil, invoker))
	require.Empty(t, seen.Get(HeaderActor))
}
