package clienttest

import (
	"context"
	"reflect"
	"testing"
)

type req struct{}
type resp struct{}

type api interface {
	Do(context.Context, *req) (*resp, error)
	mustEmbedAPI()
}

type impl struct {
	called int
}

func (*impl) mustEmbedAPI() {}

func (i *impl) Do(context.Context, *req) (*resp, error) {
	i.called++
	return &resp{}, nil
}

type panicky struct{}

func (*panicky) mustEmbedAPI() {}

func (*panicky) Do(context.Context, *req) (*resp, error) {
	panic("zero-value request is not valid — fine for a routing smoke")
}

func TestEveryUnary_InvokesEveryMethod(t *testing.T) {
	var i impl
	EveryUnary(context.Background(), t, &i, reflect.TypeOf((*api)(nil)).Elem())
	if i.called != 1 {
		t.Fatalf("Do called %d times, want 1", i.called)
	}
}

func TestEveryUnary_ToleratesHandlerPanic(t *testing.T) {
	// A panicking far side must not abort the walk; the recover inside
	// EveryUnary keeps the test binary alive.
	EveryUnary(context.Background(), t, &panicky{}, reflect.TypeOf((*api)(nil)).Elem())
}
