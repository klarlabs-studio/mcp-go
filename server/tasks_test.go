package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

func TestTaskManager(t *testing.T) {
	t.Run("creates and registers task", func(t *testing.T) {
		mgr := NewTaskManager()

		mgr.RegisterTask("test-task", TaskSpec{
			Name:        "test-task",
			Description: "A test task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return &TaskResult{Data: "test-result"}, nil
			},
		})

		names := mgr.ListTaskNames()
		if len(names) != 1 || names[0] != "test-task" {
			t.Errorf("ListTaskNames() = %v, want [test-task]", names)
		}
	})

	t.Run("creates task", func(t *testing.T) {
		mgr := NewTaskManager()

		mgr.RegisterTask("long-running", TaskSpec{
			Name:        "long-running",
			Description: "A long running task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				time.Sleep(10 * time.Millisecond)
				return &TaskResult{Data: "completed"}, nil
			},
		})

		task, err := mgr.CreateTask(context.Background(), CreateTaskRequest{
			Name:   "long-running",
			Params: map[string]any{"key": "value"},
		})
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		if task == nil {
			t.Fatal("CreateTask() returned nil task")
		}

		if task.Status != TaskStatusQueued {
			t.Errorf("initial task status = %v, want queued", task.Status)
		}

		if task.ID == "" {
			t.Error("task.ID should not be empty")
		}

		time.Sleep(20 * time.Millisecond)

		updated, err := mgr.GetTask(task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}

		if updated.Status != TaskStatusCompleted {
			t.Errorf("task status after completion = %v, want completed", updated.Status)
		}
	})

	t.Run("handles task error", func(t *testing.T) {
		mgr := NewTaskManager()

		mgr.RegisterTask("failing", TaskSpec{
			Name:        "failing",
			Description: "A failing task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return nil, &taskError{msg: "something went wrong"}
			},
		})

		task, err := mgr.CreateTask(context.Background(), CreateTaskRequest{
			Name: "failing",
		})
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		updated, err := mgr.GetTask(task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}

		if updated.Status != TaskStatusFailed {
			t.Errorf("task status = %v, want failed", updated.Status)
		}

		if updated.Result == nil || updated.Result.Error == "" {
			t.Error("expected error in result")
		}
	})

	t.Run("cancels task", func(t *testing.T) {
		mgr := NewTaskManager()

		started := make(chan struct{})
		mgr.RegisterTask("cancelable", TaskSpec{
			Name:        "cancelable",
			Description: "A cancelable task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				close(started)
				time.Sleep(100 * time.Millisecond)
				return &TaskResult{Data: "done"}, nil
			},
		})

		task, _ := mgr.CreateTask(context.Background(), CreateTaskRequest{
			Name: "cancelable",
		})

		<-started
		time.Sleep(5 * time.Millisecond)

		err := mgr.CancelTask(task.ID)
		if err != nil {
			t.Fatalf("CancelTask() error = %v", err)
		}

		updated, _ := mgr.GetTask(task.ID)
		if updated.Status != TaskStatusCanceled {
			t.Errorf("task status = %v, want canceled", updated.Status)
		}
	})

	t.Run("handles unknown task name", func(t *testing.T) {
		mgr := NewTaskManager()

		_, err := mgr.CreateTask(context.Background(), CreateTaskRequest{
			Name: "nonexistent",
		})
		if err == nil {
			t.Error("CreateTask() expected error for unknown task")
		}
	})

	t.Run("lists tasks", func(t *testing.T) {
		mgr := NewTaskManager()

		mgr.RegisterTask("task1", TaskSpec{
			Name:        "task1",
			Description: "Task 1",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return &TaskResult{Data: "result1"}, nil
			},
		})

		mgr.RegisterTask("task2", TaskSpec{
			Name:        "task2",
			Description: "Task 2",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return &TaskResult{Data: "result2"}, nil
			},
		})

		_, _ = mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "task1"})
		_, _ = mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "task2"})

		resp, err := mgr.ListTasks(10, "")
		if err != nil {
			t.Fatalf("ListTasks() error = %v", err)
		}

		if len(resp.Tasks) != 2 {
			t.Errorf("ListTasks() returned %d tasks, want 2", len(resp.Tasks))
		}
	})

	t.Run("handles tasks/request protocol method", func(t *testing.T) {
		mgr := NewTaskManager()

		mgr.RegisterTask("proto-task", TaskSpec{
			Name:        "proto-task",
			Description: "Protocol task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return &TaskResult{Data: "proto-result"}, nil
			},
		})

		params := json.RawMessage(`{"name":"proto-task","params":{}}`)
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tasks/create",
			Params:  params,
		}

		resp, err := mgr.HandleRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("HandleRequest() error = %v", err)
		}

		if resp.Error != nil {
			t.Errorf("HandleRequest() error = %v", resp.Error)
		}
	})
}

type taskError struct {
	msg string
}

func (e *taskError) Error() string {
	return e.msg
}

func TestTaskRegistry(t *testing.T) {
	t.Run("registers and retrieves handler", func(t *testing.T) {
		registry := newTaskRegistry()

		spec := TaskSpec{
			Name:        "test",
			Description: "Test task",
			Handler: func(ctx context.Context, input map[string]any) (*TaskResult, error) {
				return nil, nil
			},
		}

		registry.Register("test", spec)

		retrieved, ok := registry.GetHandler("test")
		if !ok {
			t.Error("GetHandler() returned false, expected true")
		}

		if retrieved.Name != "test" {
			t.Errorf("retrieved.Name = %v, want test", retrieved.Name)
		}
	})

	t.Run("returns false for unknown handler", func(t *testing.T) {
		registry := newTaskRegistry()

		_, ok := registry.GetHandler("nonexistent")
		if ok {
			t.Error("GetHandler() returned true for nonexistent handler")
		}
	})
}

func TestServer_RegisterTask(t *testing.T) {
	srv := New(Info{
		Name:    "test-server",
		Version: "1.0.0",
		Capabilities: Capabilities{
			Tools: true,
		},
	})

	srv.RegisterTask("my-task", "My test task", func(ctx context.Context, input map[string]any) (*TaskResult, error) {
		return &TaskResult{Data: "result"}, nil
	})

	if srv.tasks == nil {
		t.Fatal("srv.tasks should not be nil after RegisterTask")
	}

	names := srv.tasks.ListTaskNames()
	if len(names) != 1 || names[0] != "my-task" {
		t.Errorf("ListTaskNames() = %v, want [my-task]", names)
	}
}
