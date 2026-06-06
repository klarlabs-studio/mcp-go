package transport_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/transport"
)

func TestWebSocket(t *testing.T) {
	t.Run("handles requests over websocket", func(t *testing.T) {
		handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, map[string]string{"result": "ok"}), nil
		})

		ws := transport.NewWebSocket(":0")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start server
		errChan := make(chan error, 1)
		var serverStarted sync.WaitGroup
		serverStarted.Add(1)

		go func() {
			serverStarted.Done()
			errChan <- ws.Serve(ctx, handler)
		}()

		serverStarted.Wait()
		time.Sleep(50 * time.Millisecond) // Give server time to start

		// Test is covered by integration tests below
		cancel()
	})
}

func TestWebSocket_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("full request-response cycle", func(t *testing.T) {
		handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			switch req.Method {
			case "ping":
				return protocol.NewResponse(req.ID, map[string]any{}), nil
			case "echo":
				var params map[string]string
				json.Unmarshal(req.Params, &params)
				return protocol.NewResponse(req.ID, params), nil
			default:
				return nil, protocol.NewMethodNotFound(req.Method)
			}
		})

		ws := transport.NewWebSocket(":18765")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- ws.Serve(ctx, handler)
		}()

		// Wait for server to start
		time.Sleep(100 * time.Millisecond)

		// Connect WebSocket client
		conn, httpResp, err := websocket.DefaultDialer.Dial("ws://localhost:18765/", nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		if httpResp != nil && httpResp.Body != nil {
			_ = httpResp.Body.Close()
		}
		defer conn.Close()

		// Send ping request
		pingReq := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		}

		if err := conn.WriteJSON(pingReq); err != nil {
			t.Fatalf("failed to send: %v", err)
		}

		// Read response
		var resp protocol.Response
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}

		// Send echo request
		echoReq := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "echo",
			Params:  json.RawMessage(`{"message": "hello"}`),
		}

		if err := conn.WriteJSON(echoReq); err != nil {
			t.Fatalf("failed to send echo: %v", err)
		}

		var echoResp protocol.Response
		if err := conn.ReadJSON(&echoResp); err != nil {
			t.Fatalf("failed to read echo: %v", err)
		}

		if echoResp.Error != nil {
			t.Errorf("unexpected error: %v", echoResp.Error)
		}

		// Result is decoded as map[string]interface{} from JSON
		result, ok := echoResp.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map result, got %T", echoResp.Result)
		}
		if result["message"] != "hello" {
			t.Errorf("expected message 'hello', got %v", result["message"])
		}
	})

	t.Run("handles multiple clients", func(t *testing.T) {
		var mu sync.Mutex
		clientCount := 0

		handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			mu.Lock()
			clientCount++
			count := clientCount
			mu.Unlock()
			return protocol.NewResponse(req.ID, map[string]int{"client": count}), nil
		})

		ws := transport.NewWebSocket(":18766")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			ws.Serve(ctx, handler)
		}()

		time.Sleep(100 * time.Millisecond)

		// Connect multiple clients
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				conn, httpResp, err := websocket.DefaultDialer.Dial("ws://localhost:18766/", nil)
				if err != nil {
					t.Errorf("failed to connect: %v", err)
					return
				}
				if httpResp != nil && httpResp.Body != nil {
					_ = httpResp.Body.Close()
				}
				defer conn.Close()

				req := protocol.Request{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Method:  "test",
				}

				if err := conn.WriteJSON(req); err != nil {
					t.Errorf("failed to send: %v", err)
					return
				}

				var resp protocol.Response
				if err := conn.ReadJSON(&resp); err != nil {
					t.Errorf("failed to read: %v", err)
					return
				}
			}()
		}

		wg.Wait()

		mu.Lock()
		if clientCount != 3 {
			t.Errorf("expected 3 clients, got %d", clientCount)
		}
		mu.Unlock()
	})

	t.Run("sends notifications for progress", func(t *testing.T) {
		handler := transport.HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			// Get notification sender and send progress
			if sender := transport.NotificationSenderFromContext(ctx); sender != nil {
				sender.SendNotification("notifications/progress", map[string]any{
					"progressToken": "test-token",
					"progress":      50,
					"total":         100,
				})
			}
			return protocol.NewResponse(req.ID, "done"), nil
		})

		ws := transport.NewWebSocket(":18767")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			ws.Serve(ctx, handler)
		}()

		time.Sleep(100 * time.Millisecond)

		conn, httpResp, err := websocket.DefaultDialer.Dial("ws://localhost:18767/", nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		if httpResp != nil && httpResp.Body != nil {
			_ = httpResp.Body.Close()
		}
		defer conn.Close()

		req := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		if err := conn.WriteJSON(req); err != nil {
			t.Fatalf("failed to send: %v", err)
		}

		// Read progress notification
		var notif transport.Notification
		if err := conn.ReadJSON(&notif); err != nil {
			t.Fatalf("failed to read notification: %v", err)
		}

		if notif.Method != "notifications/progress" {
			t.Errorf("expected progress notification, got %s", notif.Method)
		}

		// Read response
		var resp protocol.Response
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
	})
}
