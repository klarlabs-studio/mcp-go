package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// generateTestCert builds an in-memory ECDSA cert + key valid for
// "localhost" + "127.0.0.1". Lets the TLS option tests run hermetic
// without filesystem fixtures.
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  nil,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	return cert
}

func TestWithTLSConfig_StoresConfig(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	tr := NewHTTP(":0", WithTLSConfig(cfg))
	if tr.tlsConfig != cfg {
		t.Errorf("WithTLSConfig did not store the supplied config")
	}
}

func TestHTTP_ServesOverTLSWhenConfigured(t *testing.T) {
	cert := generateTestCert(t)
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	tr := NewHTTP("127.0.0.1:0",
		WithTLSConfig(cfg),
		WithReadTimeout(2*time.Second),
		WithWriteTimeout(2*time.Second),
	)
	handler := HandlerFunc(func(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- tr.Serve(ctx, handler) }()

	// Wait until the listener has bound.
	deadline := time.Now().Add(2 * time.Second)
	for tr.ListenAddr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tr.ListenAddr() == "" {
		t.Fatal("server never bound an address")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	resp, err := client.Post("https://"+tr.ListenAddr()+"/mcp", "application/json", body)
	if err != nil {
		t.Fatalf("https POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("https status %d, body=%s", resp.StatusCode, raw)
	}
	// Verify the response is parseable JSON-RPC — proves TLS handshake
	// + handler chain completed end-to-end.
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["jsonrpc"] != "2.0" {
		t.Errorf("unexpected response: %v", out)
	}

	cancel()
	<-errCh
}

func TestHTTP_ServesPlainWhenNoTLS(t *testing.T) {
	// Regression: ensure the non-TLS path still works after the
	// ServeTLS branch was added.
	tr := NewHTTP("127.0.0.1:0")
	handler := HandlerFunc(func(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- tr.Serve(ctx, handler) }()

	deadline := time.Now().Add(2 * time.Second)
	for tr.ListenAddr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	resp, err := http.Post("http://"+tr.ListenAddr()+"/mcp", "application/json", body)
	if err != nil {
		t.Fatalf("http POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("plain status %d", resp.StatusCode)
	}
	cancel()
	<-errCh
}

func TestWithWebSocketTLSConfig_StoresConfig(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	ws := NewWebSocket(":0", WithWebSocketTLSConfig(cfg))
	if ws.tlsConfig != cfg {
		t.Errorf("WithWebSocketTLSConfig did not store the supplied config")
	}
}
