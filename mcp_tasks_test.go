package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func taskReq(t *testing.T, method string, params any) *protocol.Request {
	t.Helper()
	return &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  mustParams(t, params),
	}
}

// TestTaskAugmentation_Flow drives the full 2025-11-25 task lifecycle: an
// augmented tools/call returns a working CreateTaskResult, tasks/get reports
// working while the handler is gated, and tasks/result blocks until completion
// and returns the underlying result tagged with the related-task meta.
func TestTaskAugmentation_Flow(t *testing.T) {
	release := make(chan struct{})
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	srv.Tool("slow").
		Description("gated tool").
		TaskSupport(TaskSupportOptional).
		Handler(func(_ context.Context, i in) (string, error) {
			<-release
			return "done:" + i.X, nil
		})
	handler := newRequestHandler(srv)

	// 1. Augmented call → CreateTaskResult (status working).
	resp, err := handler.HandleRequest(context.Background(),
		taskReq(t, protocol.MethodToolsCall, map[string]any{
			"name": "slow", "arguments": map[string]any{"x": "y"}, fieldTask: map[string]any{"ttl": 60000},
		}))
	if err != nil {
		t.Fatalf("augmented call: %v", err)
	}
	task, ok := resp.Result.(map[string]any)[fieldTask].(*AugTask)
	if !ok {
		t.Fatalf("expected CreateTaskResult with *AugTask, got %#v", resp.Result)
	}
	if task.Status != TaskSupportWorkingStatus {
		t.Fatalf("expected working, got %q", task.Status)
	}
	id := task.TaskID

	// 2. tasks/get while gated → still working.
	getResp, err := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksGet, map[string]any{fieldTaskID: id}))
	if err != nil {
		t.Fatalf("tasks/get: %v", err)
	}
	if got := getResp.Result.(*AugTask); got.Status != TaskSupportWorkingStatus {
		t.Fatalf("expected working before release, got %q", got.Status)
	}

	// 3. Release; tasks/result blocks until terminal and returns the result.
	close(release)
	resResp, err := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksResult, map[string]any{fieldTaskID: id}))
	if err != nil {
		t.Fatalf("tasks/result: %v", err)
	}
	res := resResp.Result.(map[string]any)
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), "done:y") {
		t.Errorf("expected tool output in result, got %s", raw)
	}
	meta, _ := res["_meta"].(map[string]any)
	rel, _ := meta[relatedTaskMetaKey].(map[string]any)
	if rel == nil || rel["taskId"] != id {
		t.Errorf("expected related-task meta with taskId %s, got %v", id, meta)
	}

	// 4. tasks/get now completed.
	getResp2, _ := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksGet, map[string]any{fieldTaskID: id}))
	if got := getResp2.Result.(*AugTask); got.Status != "completed" {
		t.Errorf("expected completed, got %q", got.Status)
	}
}

// TaskSupportWorkingStatus is the initial task status literal, kept local to the
// test to avoid exporting an internal constant.
const TaskSupportWorkingStatus = "working"

func TestTaskAugmentation_Rejections(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	srv.Tool("plain").Description("no tasks").
		Handler(func(_ in) (string, error) { return "ok", nil })
	srv.Tool("must").Description("task required").TaskSupport(TaskSupportRequired).
		Handler(func(_ in) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	// task on a forbidden tool → -32601.
	_, err := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodToolsCall, map[string]any{
		"name": "plain", "arguments": map[string]any{"x": "y"}, fieldTask: map[string]any{},
	}))
	assertCode(t, err, protocol.CodeMethodNotFound, "task on forbidden tool")

	// plain call on a required-task tool → -32601.
	_, err = handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodToolsCall, map[string]any{
		"name": "must", "arguments": map[string]any{"x": "y"},
	}))
	assertCode(t, err, protocol.CodeMethodNotFound, "plain call on required-task tool")

	// tasks/get unknown id → -32602.
	_, err = handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksGet, map[string]any{fieldTaskID: "nope"}))
	assertCode(t, err, protocol.CodeInvalidParams, "unknown task get")
}

func TestTaskAugmentation_CancelTerminalRejected(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	done := make(chan struct{})
	srv.Tool("t").Description("").TaskSupport(TaskSupportOptional).
		Handler(func(_ context.Context, _ in) (string, error) { <-done; return "ok", nil })
	handler := newRequestHandler(srv)

	resp, _ := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodToolsCall, map[string]any{
		"name": "t", "arguments": map[string]any{"x": "y"}, fieldTask: map[string]any{},
	}))
	id := resp.Result.(map[string]any)[fieldTask].(*AugTask).TaskID

	// Cancel the working task → cancelled.
	cResp, err := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksCancel, map[string]any{fieldTaskID: id}))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cResp.Result.(*AugTask).Status != "cancelled" {
		t.Fatalf("expected cancelled, got %q", cResp.Result.(*AugTask).Status)
	}
	// Cancel again (terminal) → -32602.
	_, err = handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodTasksCancel, map[string]any{fieldTaskID: id}))
	assertCode(t, err, protocol.CodeInvalidParams, "cancel terminal task")
	close(done)
}

func TestTaskCapability_And_ExecutionTaskSupport(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	srv.Tool("t").Description("").TaskSupport(TaskSupportOptional).
		Handler(func(_ in) (string, error) { return "ok", nil })
	res := initResult(t, srv, "2025-11-25")
	caps := res["capabilities"].(map[string]any)
	if _, ok := caps["tasks"]; !ok {
		t.Errorf("expected tasks capability advertised, got %v", caps)
	}

	handler := newRequestHandler(srv)
	lResp, _ := handler.HandleRequest(context.Background(), taskReq(t, protocol.MethodToolsList, map[string]any{}))
	tool := lResp.Result.(map[string]any)["tools"].([]map[string]any)[0]
	exec, _ := tool["execution"].(map[string]any)
	if exec == nil || exec["taskSupport"] != "optional" {
		t.Errorf("expected execution.taskSupport=optional in tools/list, got %v", tool["execution"])
	}
}

func assertCode(t *testing.T, err error, code int, ctx string) {
	t.Helper()
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != code {
		t.Fatalf("%s: expected error code %d, got %v", ctx, code, err)
	}
}
