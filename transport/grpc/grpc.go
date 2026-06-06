// Package grpc provides MCP transport over gRPC.
//
// This transport enables MCP communication using gRPC's bidirectional streaming,
// providing benefits like binary encoding, built-in flow control, and native
// support for enterprise infrastructure.
//
// Usage:
//
//	srv := mcp.NewServer("my-server", "1.0.0")
//	srv.Tool("search").Handler(searchHandler)
//
//	grpcTransport := grpc.NewGRPC(":50051")
//	if err := grpcTransport.Serve(ctx, handler); err != nil {
//	    log.Fatal(err)
//	}
package grpc

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"go.klarlabs.de/mcp/transport"
	pb "go.klarlabs.de/mcp/transport/grpc/mcpv1"
)

// GRPC implements MCP transport over gRPC bidirectional streaming.
type GRPC struct {
	addr            string
	actualAddr      string // Set after Listen when :0 is used
	server          *grpc.Server
	shutdownTimeout time.Duration
	drainDelay      time.Duration
	serverOpts      []grpc.ServerOption

	mu      sync.RWMutex
	streams map[*streamClient]struct{}
}

// streamClient represents a single gRPC stream connection.
type streamClient struct {
	stream pb.MCP_ConnectServer
}

// Option configures a GRPC transport.
type Option func(*GRPC)

// WithShutdownTimeout sets the maximum time to wait for graceful shutdown.
func WithShutdownTimeout(d time.Duration) Option {
	return func(g *GRPC) {
		g.shutdownTimeout = d
	}
}

// WithDrainDelay sets the delay before starting connection draining.
// This allows load balancers to remove the server from rotation.
func WithDrainDelay(d time.Duration) Option {
	return func(g *GRPC) {
		g.drainDelay = d
	}
}

// WithServerOptions sets additional gRPC server options.
// Use this to configure interceptors, max message sizes, custom
// credentials, etc.
func WithServerOptions(opts ...grpc.ServerOption) Option {
	return func(g *GRPC) {
		g.serverOpts = append(g.serverOpts, opts...)
	}
}

// WithTLSConfig is a shorthand for embedded TLS termination. It is
// equivalent to passing grpc.Creds(credentials.NewTLS(cfg)) through
// WithServerOptions. Bring your own certificate loading + rotation
// strategy; set ClientCAs + ClientAuth on the config for mTLS, which
// is the common case for service-mesh and regulated deployments.
//
// Use the underlying WithServerOptions when you need credentials that
// aren't a static *tls.Config (SPIFFE workload API, ALTS, etc.).
func WithTLSConfig(cfg *tls.Config) Option {
	return func(g *GRPC) {
		g.serverOpts = append(g.serverOpts, grpc.Creds(credentials.NewTLS(cfg)))
	}
}

// NewGRPC creates a new gRPC transport.
func NewGRPC(addr string, opts ...Option) *GRPC {
	g := &GRPC{
		addr:            addr,
		shutdownTimeout: 30 * time.Second,
		drainDelay:      0,
		streams:         make(map[*streamClient]struct{}),
	}

	for _, opt := range opts {
		opt(g)
	}

	return g
}

// Addr returns the transport address.
// After Serve is called with :0, this returns the actual bound address.
func (g *GRPC) Addr() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.actualAddr != "" {
		return g.actualAddr
	}
	return g.addr
}

// Serve starts the gRPC server and blocks until ctx is canceled.
func (g *GRPC) Serve(ctx context.Context, handler transport.Handler) error {
	lis, err := net.Listen("tcp", g.addr)
	if err != nil {
		return err
	}

	// Update actual address with bound port (important when :0 is used)
	g.mu.Lock()
	g.actualAddr = lis.Addr().String()
	g.mu.Unlock()

	// Create gRPC server with options
	g.server = grpc.NewServer(g.serverOpts...)

	// Register MCP service
	svc := &mcpService{
		grpc:    g,
		handler: handler,
	}
	pb.RegisterMCPServer(g.server, svc)

	errCh := make(chan error, 1)
	go func() {
		if err := g.server.Serve(lis); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return g.shutdown()
	case err := <-errCh:
		return err
	}
}

// shutdown performs graceful shutdown with drain delay and timeout.
func (g *GRPC) shutdown() error {
	// Wait for load balancers to remove server from rotation
	if g.drainDelay > 0 {
		time.Sleep(g.drainDelay)
	}

	// Attempt graceful stop with timeout
	done := make(chan struct{})
	go func() {
		g.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(g.shutdownTimeout):
		// Force stop if graceful shutdown times out
		g.server.Stop()
		return nil
	}
}

// addStream registers a stream client.
func (g *GRPC) addStream(client *streamClient) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.streams[client] = struct{}{}
}

// removeStream unregisters a stream client.
func (g *GRPC) removeStream(client *streamClient) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.streams, client)
}

// mcpService implements the MCP gRPC service.
type mcpService struct {
	pb.UnimplementedMCPServer
	grpc    *GRPC
	handler transport.Handler
}

// Connect handles bidirectional MCP communication.
func (s *mcpService) Connect(stream pb.MCP_ConnectServer) error {
	client := &streamClient{stream: stream}
	s.grpc.addStream(client)
	defer s.grpc.removeStream(client)

	return handleStream(stream.Context(), stream, s.handler)
}
