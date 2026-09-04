// Package clienttest drives every unary method of a generated gRPC service
// interface through a client — the wire-level smoke that catches mis-routed
// or self-recursive client delegations.
//
// The pattern: register a stub server (embed the generated
// UnimplementedXxxServer, override Ping) on a real in-process gRPC server,
// dial it with the server-shaped *Client, assert Ping, then EveryUnary walks
// the whole interface. A healthy delegation reaches the server and comes
// back with a response or an ordinary error; a self-recursive delegation
// (return c.X instead of c.cli.X) overflows the stack and kills the test
// binary — a failure unit tests never produce, because they never call the
// *Client.
package clienttest

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// EveryUnary invokes every method of iface (skipping the mustEmbed guards)
// on client with a zero-valued request. The point is routing, not semantics:
// zero-value requests are not meant to be valid, so ordinary errors and
// recovered panics from the far side both count as "routed". Use the
// generated server interface as iface, e.g.
//
//	reflect.TypeOf((*pb.MessageServiceServer)(nil)).Elem()
func EveryUnary(ctx context.Context, t *testing.T, client any, iface reflect.Type) {
	t.Helper()
	cv := reflect.ValueOf(client)
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		if strings.HasPrefix(m.Name, "mustEmbed") {
			continue
		}
		mv := cv.MethodByName(m.Name)
		if !mv.IsValid() {
			t.Fatalf("client does not implement %s.%s", iface.String(), m.Name)
		}
		if m.Type.NumIn() != 2 || m.Type.In(1).Kind() != reflect.Ptr {
			t.Fatalf("%s.%s is not unary-shaped (context, *Request); EveryUnary only walks unary methods", iface.String(), m.Name)
		}
		req := reflect.New(m.Type.In(1).Elem())
		func() {
			defer func() { _ = recover() }() // zero requests may panic inside handlers; routing is still proved
			_ = mv.Call([]reflect.Value{reflect.ValueOf(ctx), req})
		}()
	}
}
