package middleware_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
)

func TestSizeLimit(t *testing.T) {
	t.Run("allows requests within limit", func(t *testing.T) {
		m := middleware.SizeLimit(1024) // 1KB limit

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
			Params:  json.RawMessage(`{"small": "data"}`),
		}

		resp, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("rejects requests exceeding limit", func(t *testing.T) {
		m := middleware.SizeLimit(50) // 50 byte limit

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		// Create a large payload
		largeData := strings.Repeat("x", 100)
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
			Params:  json.RawMessage(`{"data": "` + largeData + `"}`),
		}

		_, err := handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected size limit error")
		}

		protoErr, ok := err.(*protocol.Error)
		if !ok {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if protoErr.Code != protocol.CodeInvalidRequest {
			t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
		}

		if !strings.Contains(protoErr.Message, "exceeds limit") {
			t.Errorf("expected size limit message, got: %s", protoErr.Message)
		}
	})

	t.Run("allows nil params", func(t *testing.T) {
		m := middleware.SizeLimit(10) // Very small limit

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
			Params:  nil,
		}

		resp, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("uses KB and MB constants", func(t *testing.T) {
		if middleware.KB != 1024 {
			t.Errorf("expected KB = 1024, got %d", middleware.KB)
		}
		if middleware.MB != 1024*1024 {
			t.Errorf("expected MB = %d, got %d", 1024*1024, middleware.MB)
		}
	})
}
