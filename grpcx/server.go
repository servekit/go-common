// Package grpcx provides a gRPC server with optional HTTP gateway support,
// integrated with signalx for signal-based lifecycle management.
//
// Server implements the signalx.Service interface (Start + Stop), so it can be
// passed directly to signalx.Run. The typical usage pattern is:
//
//	srv := grpcx.New(cfg, registerGRPC, registerMW, interceptors...)
//	srv.Run()  // internally calls signalx.Run(s): Start → block on signal → Stop
//
// For callers that need to manage lifecycle themselves (e.g. embedding in a
// lifecycle.Manager), use Start/Stop directly instead of Run:
//
//	srv := grpcx.New(cfg, registerGRPC, registerMW)
//	srv.Start()          // start gRPC + gateway listeners (non-blocking)
//	// ... do other work ...
//	srv.Stop()           // graceful shutdown
package grpcx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/servekit/go-common/signalx"
)

type (
	// ServerConfig holds listener addresses for the gRPC server and optional HTTP gateway.
	ServerConfig struct {
		GRPCAddr    string
		GatewayAddr string // empty = no gateway
		// GatewayWrap, when set, wraps the gateway's HTTP handler — the hook
		// for edge middleware (auth, rate limiting, request logging). It
		// sees every request before transcoding, including routes mounted
		// directly on the mux via HandlePath. nil = no wrapping.
		GatewayWrap func(http.Handler) http.Handler
		// ServerOptions are passed to grpc.NewServer in addition to the
		// interceptor chain. Use this for limits that must be set on the
		// server itself — e.g. grpc.MaxRecvMsgSize when a service accepts
		// large inline payloads (file uploads, attachment bytes).
		ServerOptions []grpc.ServerOption
	}

	// RegisterGRPCFunc registers a gRPC service implementation on the server.
	RegisterGRPCFunc func(*grpc.Server)

	// RegisterGatewayFunc registers HTTP gateway handlers via grpc-gateway.
	RegisterGatewayFunc func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error

	// Server manages a gRPC server and optional HTTP gateway with graceful shutdown.
	// It implements signalx.Service, so it can be used with signalx.Run(s).
	Server struct {
		cfg          *ServerConfig
		registerGRPC RegisterGRPCFunc
		registerGW   RegisterGatewayFunc
		interceptors []grpc.UnaryServerInterceptor
		grpcServer   *grpc.Server
		healthServer *health.Server
		cancel       context.CancelFunc
	}
)

// New creates a new Server.
// registerMW may be nil if no HTTP gateway is needed.
func New(cfg *ServerConfig, registerGRPC RegisterGRPCFunc, registerGW RegisterGatewayFunc, interceptors ...grpc.UnaryServerInterceptor) *Server {
	return &Server{
		cfg:          cfg,
		registerGRPC: registerGRPC,
		registerGW:   registerGW,
		interceptors: interceptors,
	}
}

// Start starts the gRPC listener and optional HTTP gateway.
// Both run in background goroutines, so Start returns quickly.
// Implements signalx.Service.Start.
func (s *Server) Start() error {
	if err := s.startGRPC(); err != nil {
		return fmt.Errorf("start gRPC: %w", err)
	}
	if s.cfg.GatewayAddr != "" && s.registerGW != nil {
		if err := s.startGateway(); err != nil {
			return fmt.Errorf("start gateway: %w", err)
		}
	}
	return nil
}

// Stop performs a graceful shutdown: sets health to NOT_SERVING, stops the
// gRPC server, and cancels the gateway context.
// Implements signalx.Service.Stop.
func (s *Server) Stop() error {
	if s.healthServer != nil {
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.cancel != nil {
		s.cancel()
	}
	slog.Info("server stopped")
	return nil
}

// Run is the all-in-one entry point: starts the server via Start, blocks the
// calling goroutine until SIGINT or SIGTERM, then shuts down via Stop.
//
// Internally delegates to signalx.Run(s), where s (the Server itself) is
// injected as the signalx.Service. This is the recommended way to use Server
// in a standalone service.
func (s *Server) Run() error {
	return signalx.Run(s)
}

// startGRPC creates the gRPC server, registers services and health checks,
// then begins serving in a background goroutine.
func (s *Server) startGRPC() error {
	opts := make([]grpc.ServerOption, 0, 1+len(s.cfg.ServerOptions))
	if len(s.interceptors) > 0 {
		opts = append(opts, grpc.ChainUnaryInterceptor(s.interceptors...))
	}
	opts = append(opts, s.cfg.ServerOptions...)
	s.grpcServer = grpc.NewServer(opts...)

	s.registerGRPC(s.grpcServer)

	s.healthServer = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthServer)
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		slog.Info("gRPC server listening", "addr", s.cfg.GRPCAddr)
		if err := s.grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC serve", "error", err)
		}
	}()
	return nil
}

// startGateway registers the grpc-gateway handlers synchronously, then starts
// the HTTP server in a background goroutine. Registration errors are returned
// immediately so callers can fail fast.
func (s *Server) startGateway() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	mux := runtime.NewServeMux()
	gwOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := s.registerGW(ctx, mux, s.cfg.GRPCAddr, gwOpts); err != nil {
		cancel()
		return fmt.Errorf("register gateway: %w", err)
	}

	handler := http.Handler(mux)
	if s.cfg.GatewayWrap != nil {
		handler = s.cfg.GatewayWrap(handler)
	}

	go func() {
		slog.Info("HTTP gateway listening", "addr", s.cfg.GatewayAddr)
		if err := http.ListenAndServe(s.cfg.GatewayAddr, handler); err != nil {
			slog.Error("HTTP gateway serve", "error", err)
		}
	}()
	return nil
}
