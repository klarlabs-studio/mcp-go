package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

func TestModern_RequiredToolWithoutTasksExtension(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("must").Description("task required").TaskSupport(TaskSupportRequired).
		Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	_, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernParams(t, protocol.ModernVersion, map[string]any{
			"name": "must", "arguments": map[string]any{},
		}),
	})
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeMissingRequiredClientCapability {
		t.Fatalf("got %v, want -32021 missing tasks extension", err)
	}
}

func TestModern_TaskElicitationRoundTrip(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Description("elicit during task").TaskSupport(TaskSupportRequired).
		Handler(func(ctx context.Context, _ struct{}) (string, error) {
			el := server.ElicitFromContext(ctx)
			res, err := el.Elicit(ctx, &server.ElicitRequest{
				Message:         "name?",
				RequestedSchema: map[string]any{"type": "object"},
			})
			if err != nil {
				return "", err
			}
			return res.Content["name"].(string), nil
		})
	handler := newRequestHandler(srv)

	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernTaskParams(t, map[string]any{"name": "ask", "arguments": map[string]any{}}),
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	result, _ := resp.Result.(map[string]any)
	if result[fieldResultType] != protocol.ResultTypeTask {
		t.Fatalf("tools/call resultType = %v, want %q (must not rewrite to input_required)", result[fieldResultType], protocol.ResultTypeTask)
	}
	id, _ := result[fieldTaskID].(string)
	if id == "" {
		t.Fatalf("missing taskId: %#v", resp.Result)
	}

	got := waitTaskStatus(t, handler, id, "input_required")
	// Inspect the wire encoding: inputRequests is a map of request id to a
	// {"method", "params"} request object (SEP-2663 reuses MRTR's InputRequests).
	wire, err := json.Marshal(got[fieldInputRequests])
	if err != nil {
		t.Fatalf("marshal inputRequests: %v", err)
	}
	var reqs map[string]struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(wire, &reqs); err != nil || len(reqs) != 1 {
		t.Fatalf("inputRequests = %s (%v), want one outstanding elicitation", wire, err)
	}
	var reqID string
	for k, r := range reqs {
		reqID = k
		if r.Method != protocol.MethodElicitationCreate || r.Params["message"] != "name?" {
			t.Fatalf("inputRequests[%q] = %+v, want elicitation/create asking name?", k, r)
		}
	}

	_, err = handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: protocol.MethodTasksUpdate,
		Params: modernTaskParams(t, map[string]any{
			fieldTaskID: id,
			fieldInputResponses: map[string]any{
				reqID: map[string]any{"action": "accept", "content": map[string]any{"name": "Luca"}},
			},
		}),
	})
	if err != nil {
		t.Fatalf("tasks/update: %v", err)
	}

	done := waitTaskStatus(t, handler, id, "completed")
	raw, _ := json.Marshal(done[fieldResult])
	if !strings.Contains(string(raw), "Luca") {
		t.Errorf("expected inlined result Luca, got %s", raw)
	}
}

func TestModern_RequiredTaskCallKeepsHandleWhenHandlerElicits(t *testing.T) {
	// tools/call must return a CreateTaskResult even when the background handler
	// elicits immediately — it must not be rewritten to input_required.
	for i := 0; i < 30; i++ {
		srv := NewServer(ServerInfo{Name: "s", Version: "1"})
		srv.Tool("ask").Description("elicit").TaskSupport(TaskSupportRequired).
			Handler(func(ctx context.Context, _ struct{}) (string, error) {
				_, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{
					Message:         "?",
					RequestedSchema: map[string]any{"type": "object"},
				})
				return "", err
			})
		handler := newRequestHandler(srv)
		resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
			JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
			Params: modernTaskParams(t, map[string]any{"name": "ask", "arguments": map[string]any{}}),
		})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		result, _ := resp.Result.(map[string]any)
		if result[fieldResultType] != protocol.ResultTypeTask || result[fieldTaskID] == nil {
			t.Fatalf("iter %d: got %#v, want CreateTaskResult", i, resp.Result)
		}
	}
}

func TestModern_TaskUpdateIgnoresUnknownKeys(t *testing.T) {
	handler, id, release := newWorkingTask(t)
	defer close(release)

	_, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodTasksUpdate,
		Params: modernTaskParams(t, map[string]any{
			fieldTaskID: id,
			fieldInputResponses: map[string]any{
				"nope": map[string]any{"action": "accept"},
			},
		}),
	})
	if err != nil {
		t.Fatalf("unknown inputResponses keys must be ignored: %v", err)
	}
}

func TestSubscriptionsListen_TaskIDsRequireExtension(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)
	_, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: modernParams(t, protocol.ModernVersion, map[string]any{
			"notifications": map[string]any{"taskIds": []string{"abc"}},
		}),
	})
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeMissingRequiredClientCapability {
		t.Fatalf("got %v, want -32021", err)
	}
}

func TestNotificationsTasks_PushedOnComplete(t *testing.T) {
	release := make(chan struct{})
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("slow").Description("gated").TaskSupport(TaskSupportRequired).
		Handler(func(_ context.Context, _ struct{}) (string, error) {
			<-release
			return "done", nil
		})
	n := &taskCaptureNotifier{}
	srv.SetResourceNotifier(n)
	handler := newRequestHandler(srv)

	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernTaskParams(t, map[string]any{"name": "slow", "arguments": map[string]any{}}),
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	id := resp.Result.(map[string]any)[fieldTaskID].(string)

	listen, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodSubscriptionsListen,
		Params: modernTaskParams(t, map[string]any{
			"notifications": map[string]any{"taskIds": []string{id}},
		}),
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	subID := listen.Result.(map[string]any)["subscriptionId"].(string)
	if subID == "" {
		t.Fatal("empty subscriptionId")
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range n.snapshot() {
			if c.method == protocol.MethodTasks && c.clientID == subID {
				params, _ := c.params.(map[string]any)
				if params[fieldStatus] == "completed" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("did not receive notifications/tasks with status completed")
}

type taskNotifyCall struct {
	clientID string
	method   string
	params   any
}

type taskCaptureNotifier struct {
	mu    sync.Mutex
	calls []taskNotifyCall
}

func (n *taskCaptureNotifier) NotifyClient(clientID, method string, params any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, taskNotifyCall{clientID: clientID, method: method, params: params})
	return nil
}

func (n *taskCaptureNotifier) snapshot() []taskNotifyCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]taskNotifyCall(nil), n.calls...)
}

func waitTaskStatus(t *testing.T, handler *requestHandler, id, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got map[string]any
	for time.Now().Before(deadline) {
		resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
			JSONRPC: "2.0", ID: json.RawMessage(`9`), Method: protocol.MethodTasksGet,
			Params: modernTaskParams(t, map[string]any{fieldTaskID: id}),
		})
		if err != nil {
			t.Fatalf("tasks/get: %v", err)
		}
		got = resp.Result.(map[string]any)
		if got[fieldStatus] == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status = %v, want %s", got[fieldStatus], want)
	return got
}
