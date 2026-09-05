package grpcx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// freeAddr returns a localhost address with an OS-assigned free port.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}

// TestServer_GatewayWrap_AppliedToGatewayHandler starts a Server whose
// gateway mounts one route via mux.HandlePath, with GatewayWrap adding a
// marker header — the edge-middleware contract: the wrapper sees every
// request before the mux, HandlePath routes included.
func TestServer_GatewayWrap_AppliedToGatewayHandler(t *testing.T) {
	grpcAddr := freeAddr(t)
	gwAddr := freeAddr(t)

	registerGRPC := func(*grpc.Server) {} // no service; the route is HTTP-native
	registerGW := func(_ context.Context, mux *runtime.ServeMux, _ string, _ []grpc.DialOption) error {
		return mux.HandlePath("GET", "/ping", func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "pong")
		})
	}
	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Wrapped", "1")
			next.ServeHTTP(w, r)
		})
	}

	srv := New(&ServerConfig{
		GRPCAddr:    grpcAddr,
		GatewayAddr: gwAddr,
		GatewayWrap: wrap,
	}, registerGRPC, registerGW)

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	resp, err := waitForGet(fmt.Sprintf("http://%s/ping", gwAddr))
	if err != nil {
		t.Fatalf("get /ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Wrapped") != "1" {
		t.Errorf("X-Wrapped header = %q, want %q (GatewayWrap not applied)", resp.Header.Get("X-Wrapped"), "1")
	}
}

// waitForGet retries until the gateway listener is up (Start serves in
// background goroutines), then returns the response.
func waitForGet(url string) (*http.Response, error) {
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	return nil, lastErr
}
