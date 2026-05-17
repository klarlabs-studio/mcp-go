package grpc

import (
	"crypto/tls"
	"testing"
)

func TestWithTLSConfig_AppendsCredsServerOption(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	g := NewGRPC(":0", WithTLSConfig(cfg))
	// We can't introspect grpc.ServerOption directly (opaque type), but
	// the shorthand must append exactly one entry to serverOpts.
	if len(g.serverOpts) != 1 {
		t.Errorf("expected exactly one server option from WithTLSConfig, got %d", len(g.serverOpts))
	}
}

func TestWithTLSConfig_ComposesWithServerOptions(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	g := NewGRPC(":0",
		WithTLSConfig(cfg),
		WithServerOptions(),
	)
	if len(g.serverOpts) != 1 {
		t.Errorf("expected 1 server option (TLS only), got %d", len(g.serverOpts))
	}
}
