package grpc

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/transport"
	pb "go.klarlabs.de/mcp/transport/grpc/mcpv1"
)

func TestGRPC_Addr(t *testing.T) {
	g := NewGRPC(":50051")
	if got := g.Addr(); got != ":50051" {
		t.Errorf("Addr() = %q, want %q", got, ":50051")
	}
}

func TestGRPC_Options(t *testing.T) {
	g := NewGRPC(":50051",
		WithShutdownTimeout(10*time.Second),
		WithDrainDelay(5*time.Second),
	)

	if g.shutdownTimeout != 10*time.Second {
		t.Errorf("shutdownTimeout = %v, want %v", g.shutdownTimeout, 10*time.Second)
	}
	if g.drainDelay != 5*time.Second {
		t.Errorf("drainDelay = %v, want %v", g.drainDelay, 5*time.Second)
	}
}

func TestProtoToRequest(t *testing.T) {
	tests := []struct {
		name    string
		msg     *pb.Message
		wantID  bool
		wantMet string
	}{
		{
			name: "request with ID",
			msg: &pb.Message{
				RequestId: "123",
				Method:    "tools/call",
				Params:    []byte(`{"name":"test"}`),
			},
			wantID:  true,
			wantMet: "tools/call",
		},
		{
			name: "notification without ID",
			msg: &pb.Message{
				Method: "notifications/progress",
				Params: []byte(`{"progress":50}`),
			},
			wantID:  false,
			wantMet: "notifications/progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := protoToRequest(tt.msg)

			if req.JSONRPC != "2.0" {
				t.Errorf("JSONRPC = %q, want %q", req.JSONRPC, "2.0")
			}
			if req.Method != tt.wantMet {
				t.Errorf("Method = %q, want %q", req.Method, tt.wantMet)
			}
			if tt.wantID && req.ID == nil {
				t.Error("expected ID to be set")
			}
			if !tt.wantID && req.ID != nil {
				t.Error("expected ID to be nil")
			}
		})
	}
}

func TestResponseToProto(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		resp      *protocol.Response
		wantErr   bool
	}{
		{
			name:      "success response",
			requestID: "123",
			resp: &protocol.Response{
				Result: map[string]any{"status": "ok"},
			},
			wantErr: false,
		},
		{
			name:      "error response",
			requestID: "456",
			resp: &protocol.Response{
				Error: &protocol.Error{
					Code:    -32601,
					Message: "method not found",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := responseToProto(tt.requestID, tt.resp)

			if msg.Type != pb.MessageType_MESSAGE_TYPE_RESPONSE {
				t.Errorf("Type = %v, want RESPONSE", msg.Type)
			}
			if msg.RequestId != tt.requestID {
				t.Errorf("RequestId = %q, want %q", msg.RequestId, tt.requestID)
			}
			if tt.wantErr {
				if msg.Error == nil {
					t.Error("expected error to be set")
				}
			} else {
				if msg.Error != nil {
					t.Error("expected no error")
				}
				if msg.Result == nil {
					t.Error("expected result to be set")
				}
			}
		})
	}
}

func TestErrorToProto(t *testing.T) {
	err := &protocol.Error{
		Code:    -32602,
		Message: "invalid params",
		Data:    map[string]any{"field": "name"},
	}

	msg := errorToProto("req-1", err)

	if msg.Type != pb.MessageType_MESSAGE_TYPE_RESPONSE {
		t.Errorf("Type = %v, want RESPONSE", msg.Type)
	}
	if msg.Error == nil {
		t.Fatal("expected error to be set")
	}
	if msg.Error.Code != -32602 {
		t.Errorf("Error.Code = %d, want %d", msg.Error.Code, -32602)
	}
	if msg.Error.Message != "invalid params" {
		t.Errorf("Error.Message = %q, want %q", msg.Error.Message, "invalid params")
	}
	if msg.Error.Data == nil {
		t.Error("expected Error.Data to be set")
	}
}

func TestStreamNotificationSender(t *testing.T) {
	// Create a mock stream
	mock := &mockStream{
		sendCh: make(chan *pb.Message, 10),
	}

	sender := &streamNotificationSender{stream: mock}

	err := sender.SendNotification("notifications/progress", map[string]any{
		"progressToken": "token-1",
		"progress":      50.0,
	})
	if err != nil {
		t.Fatalf("SendNotification() error = %v", err)
	}

	// Verify the message was sent
	select {
	case msg := <-mock.sendCh:
		if msg.Type != pb.MessageType_MESSAGE_TYPE_NOTIFICATION {
			t.Errorf("Type = %v, want NOTIFICATION", msg.Type)
		}
		if msg.Method != "notifications/progress" {
			t.Errorf("Method = %q, want %q", msg.Method, "notifications/progress")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// mockStream implements pb.MCP_ConnectServer for testing.
type mockStream struct {
	pb.MCP_ConnectServer
	sendCh chan *pb.Message
	recvCh chan *pb.Message
	ctx    context.Context
}

func (m *mockStream) Send(msg *pb.Message) error {
	m.sendCh <- msg
	return nil
}

func (m *mockStream) Recv() (*pb.Message, error) {
	if m.recvCh == nil {
		return nil, io.EOF
	}
	msg, ok := <-m.recvCh
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (m *mockStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func TestGRPC_Serve_Integration(t *testing.T) {
	// Create a simple handler
	handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		switch req.Method {
		case "ping":
			return protocol.NewResponse(req.ID, map[string]any{}), nil
		case "tools/list":
			return protocol.NewResponse(req.ID, map[string]any{
				"tools": []any{},
			}), nil
		default:
			return nil, protocol.NewMethodNotFound(req.Method)
		}
	})

	// Start server
	g := NewGRPC(":0") // Use :0 for random available port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- g.Serve(ctx, handler)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect client
	conn, err := grpc.NewClient(g.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewMCPClient(conn)

	// Open bidirectional stream
	stream, err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("failed to connect stream: %v", err)
	}

	// Send ping request
	err = stream.Send(&pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_REQUEST,
		RequestId: "1",
		Method:    "ping",
	})
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// Receive response
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	if resp.Type != pb.MessageType_MESSAGE_TYPE_RESPONSE {
		t.Errorf("response type = %v, want RESPONSE", resp.Type)
	}
	if resp.RequestId != "1" {
		t.Errorf("response ID = %q, want %q", resp.RequestId, "1")
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}

	// Send tools/list request
	err = stream.Send(&pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_REQUEST,
		RequestId: "2",
		Method:    "tools/list",
	})
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// Receive response
	resp, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	if resp.RequestId != "2" {
		t.Errorf("response ID = %q, want %q", resp.RequestId, "2")
	}

	// Parse result
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if _, ok := result["tools"]; !ok {
		t.Error("expected tools in result")
	}

	// Test method not found
	err = stream.Send(&pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_REQUEST,
		RequestId: "3",
		Method:    "unknown/method",
	})
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	resp, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error != nil && resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32601)
	}

	// Close stream
	stream.CloseSend()

	// Cancel context to shutdown server
	cancel()

	// Wait for server to shutdown
	select {
	case err := <-serverErr:
		if err != nil && err != context.Canceled {
			t.Errorf("server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for server shutdown")
	}
}

func TestGRPC_Serve_Notification(t *testing.T) {
	var capturedSender transport.NotificationSender
	var mu sync.Mutex

	// Create handler that captures the notification sender
	handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		mu.Lock()
		capturedSender = transport.NotificationSenderFromContext(ctx)
		mu.Unlock()

		// Send a notification
		if capturedSender != nil {
			capturedSender.SendNotification("notifications/progress", map[string]any{
				"progressToken": "test-token",
				"progress":      50.0,
			})
		}

		return protocol.NewResponse(req.ID, map[string]any{"status": "ok"}), nil
	})

	// Start server
	g := NewGRPC(":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go g.Serve(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	// Connect client
	conn, err := grpc.NewClient(g.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewMCPClient(conn)
	stream, err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("failed to connect stream: %v", err)
	}

	// Send request
	err = stream.Send(&pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_REQUEST,
		RequestId: "1",
		Method:    "test",
	})
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// Expect notification first
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive notification: %v", err)
	}

	if msg.Type != pb.MessageType_MESSAGE_TYPE_NOTIFICATION {
		t.Errorf("expected notification, got %v", msg.Type)
	}

	// Then expect response
	msg, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive response: %v", err)
	}

	if msg.Type != pb.MessageType_MESSAGE_TYPE_RESPONSE {
		t.Errorf("expected response, got %v", msg.Type)
	}

	stream.CloseSend()
	cancel()
}

func TestGRPC_GracefulShutdown(t *testing.T) {
	handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, map[string]any{}), nil
	})

	g := NewGRPC(":0",
		WithShutdownTimeout(2*time.Second),
		WithDrainDelay(100*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)

	go func() {
		serverDone <- g.Serve(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	start := time.Now()
	cancel()

	select {
	case err := <-serverDone:
		elapsed := time.Since(start)
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error: %v", err)
		}
		// Should have waited at least drain delay
		if elapsed < 100*time.Millisecond {
			t.Errorf("shutdown too fast: %v, expected at least 100ms", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for shutdown")
	}
}
