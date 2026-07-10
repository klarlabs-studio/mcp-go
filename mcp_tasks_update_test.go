package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// newWorkingTask registers a gated tool and starts an augmented call, returning
// the handler and the working task's id (the tool blocks on release, so the task
// stays non-terminal until the caller closes it).
func newWorkingTask(t *testing.T) (*requestHandler, string, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("slow").Description("gated").TaskSupport(TaskSupportOptional).
		Handler(func(_ context.Context, _ struct{}) (string, error) {
			<-release
			return "done", nil
		})
	handler := newRequestHandler(srv)
	resp, err := handler.HandleRequest(context.Background(),
		taskReq(t, protocol.MethodToolsCall, map[string]any{
			"name": "slow", "arguments": map[string]any{}, fieldTask: map[string]any{"ttl": 60000},
		}))
	if err != nil {
		t.Fatalf("augmented call: %v", err)
	}
	task := resp.Result.(map[string]any)[fieldTask].(*AugTask)
	return handler, task.TaskID, release
}

// TestTasksUpdate_RefreshesTTL confirms tasks/update refreshes a working task's
// ttl and returns the updated task.
func TestTasksUpdate_RefreshesTTL(t *testing.T) {
	handler, id, release := newWorkingTask(t)
	defer close(release)

	resp, err := handler.HandleRequest(context.Background(),
		taskReq(t, protocol.MethodTasksUpdate, map[string]any{fieldTaskID: id, "ttl": 120000}))
	if err != nil {
		t.Fatalf("tasks/update: %v", err)
	}
	task, ok := resp.Result.(*AugTask)
	if !ok {
		t.Fatalf("expected *AugTask, got %#v", resp.Result)
	}
	if task.TTL == nil || *task.TTL != 120000 {
		t.Errorf("ttl = %v, want 120000", task.TTL)
	}
	if task.Status != "working" {
		t.Errorf("status = %q, want working (update must not change status)", task.Status)
	}
}

// TestTasksUpdate_UnknownTask confirms tasks/update on an unknown id is -32602.
func TestTasksUpdate_UnknownTask(t *testing.T) {
	handler, _, release := newWorkingTask(t)
	defer close(release)

	_, err := handler.HandleRequest(context.Background(),
		taskReq(t, protocol.MethodTasksUpdate, map[string]any{fieldTaskID: "nope", "ttl": 1000}))
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("got %v, want -32602 for unknown task", err)
	}
}

// TestTasksList_GatedOffForModern confirms tasks/list is served for a legacy
// request but returns MethodNotFound for a modern (2026-07-28) request, per the
// tasks extension retiring the listing method.
func TestTasksList_GatedOffForModern(t *testing.T) {
	handler, _, release := newWorkingTask(t)
	defer close(release)

	// Legacy tasks/list → served.
	legacy := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`9`), Method: protocol.MethodTasksList}
	if _, err := handler.HandleRequest(context.Background(), legacy); err != nil {
		t.Fatalf("legacy tasks/list should be served: %v", err)
	}

	// Modern tasks/list → MethodNotFound.
	modern := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`10`), Method: protocol.MethodTasksList,
		Params: modernParams(t, protocol.DraftVersion, nil),
	}
	_, err := handler.HandleRequest(context.Background(), modern)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeMethodNotFound {
		t.Fatalf("got %v, want MethodNotFound for modern tasks/list", err)
	}
}
