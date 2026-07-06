package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/transport"
	pb "go.klarlabs.de/mcp/transport/grpc/mcpv1"
)

// handleStream processes messages from a bidirectional gRPC stream.
func handleStream(ctx context.Context, stream pb.MCP_ConnectServer, handler transport.Handler) error {
	// Create notification sender for this stream
	sender := &streamNotificationSender{
		stream: stream,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive message from stream
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // Clean close
			}
			return err
		}

		// Process based on message type
		switch msg.Type {
		case pb.MessageType_MESSAGE_TYPE_REQUEST:
			if err := handleRequest(ctx, stream, handler, sender, msg); err != nil {
				return err
			}
		case pb.MessageType_MESSAGE_TYPE_NOTIFICATION:
			// Handle incoming notification (e.g., from client)
			handleNotification(ctx, handler, sender, msg)
		case pb.MessageType_MESSAGE_TYPE_RESPONSE:
			// Responses from client (for server-initiated requests like sampling)
			// TODO: Route to pending request handlers when bidirectional requests are implemented
		default:
			// Unknown message type, ignore
		}
	}
}

// handleRequest processes an incoming request and sends a response.
func handleRequest(ctx context.Context, stream pb.MCP_ConnectServer, handler transport.Handler, sender *streamNotificationSender, msg *pb.Message) error {
	// Convert proto message to protocol.Request
	req := protoToRequest(msg)

	// Attach notification sender to context
	reqCtx := transport.ContextWithNotificationSender(ctx, sender)

	// Attach metadata to context
	if len(msg.Metadata) > 0 {
		meta := protocol.RequestMeta(msg.Metadata)
		reqCtx = protocol.ContextWithRequestMeta(reqCtx, meta)
	}

	// Handle the request
	resp, err := handler.HandleRequest(reqCtx, req)

	// For notifications, don't send response
	if req.IsNotification() {
		return nil
	}

	// Build response message
	var respMsg *pb.Message
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			respMsg = errorToProto(msg.RequestId, mcpErr)
		} else {
			respMsg = errorToProto(msg.RequestId, protocol.NewInternalError(err.Error()))
		}
	} else if resp != nil {
		respMsg = responseToProto(msg.RequestId, resp)
	}

	if respMsg != nil {
		return stream.Send(respMsg)
	}
	return nil
}

// handleNotification processes an incoming notification (no response).
func handleNotification(ctx context.Context, handler transport.Handler, sender *streamNotificationSender, msg *pb.Message) {
	// Convert to request and handle
	req := protoToRequest(msg)
	reqCtx := transport.ContextWithNotificationSender(ctx, sender)

	// Handle notification - ignore response and errors
	_, _ = handler.HandleRequest(reqCtx, req)
}

// protoToRequest converts a protobuf Message to a protocol.Request.
func protoToRequest(msg *pb.Message) *protocol.Request {
	var id json.RawMessage
	if msg.RequestId != "" {
		// Encode the request id as a JSON string with proper escaping. Manual
		// quote-wrapping would emit malformed JSON for ids containing quotes,
		// backslashes, or control characters.
		if encoded, err := json.Marshal(msg.RequestId); err == nil {
			id = encoded
		}
	}

	return &protocol.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  msg.Method,
		Params:  msg.Params, // Already JSON-encoded bytes
	}
}

// responseToProto converts a protocol.Response to a protobuf Message.
func responseToProto(requestID string, resp *protocol.Response) *pb.Message {
	msg := &pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_RESPONSE,
		RequestId: requestID,
	}

	if resp.Error != nil {
		msg.Error = &pb.Error{
			Code:    int32(resp.Error.Code), //nolint:gosec // Error codes are JSON-RPC spec constants, always fit in int32
			Message: resp.Error.Message,
		}
		if resp.Error.Data != nil {
			if data, err := json.Marshal(resp.Error.Data); err == nil {
				msg.Error.Data = data
			}
		}
	} else if resp.Result != nil {
		if result, err := json.Marshal(resp.Result); err == nil {
			msg.Result = result
		}
	}

	return msg
}

// errorToProto creates a protobuf error response Message.
func errorToProto(requestID string, err *protocol.Error) *pb.Message {
	msg := &pb.Message{
		Type:      pb.MessageType_MESSAGE_TYPE_RESPONSE,
		RequestId: requestID,
		Error: &pb.Error{
			Code:    int32(err.Code), //nolint:gosec // Error codes are JSON-RPC spec constants, always fit in int32
			Message: err.Message,
		},
	}

	if err.Data != nil {
		if data, jsonErr := json.Marshal(err.Data); jsonErr == nil {
			msg.Error.Data = data
		}
	}

	return msg
}

// streamNotificationSender sends notifications over a gRPC stream.
type streamNotificationSender struct {
	stream pb.MCP_ConnectServer
	mu     sync.Mutex
}

// SendNotification sends a notification to the client.
func (s *streamNotificationSender) SendNotification(method string, params any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	msg := &pb.Message{
		Type:   pb.MessageType_MESSAGE_TYPE_NOTIFICATION,
		Method: method,
		Params: paramsData,
	}

	return s.stream.Send(msg)
}
