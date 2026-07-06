package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
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

// TestCancelTaskCancelsRunningContext proves CancelTask actually cancels the
// running goroutine's context (not just flips the status flag).
func TestCancelTaskCancelsRunningContext(t *testing.T) {
	mgr := NewTaskManager()

	started := make(chan struct{})
	canceled := make(chan struct{})
	mgr.RegisterTask("blocker", TaskSpec{
		Name: "blocker",
		Handler: func(ctx context.Context, _ map[string]any) (*TaskResult, error) {
			close(started)
			<-ctx.Done() // must unblock only when the context is canceled
			close(canceled)
			return nil, ctx.Err()
		},
	})

	task, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "blocker"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	<-started
	if err := mgr.CancelTask(task.ID); err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelTask did not cancel the running task's context")
	}

	updated, _ := mgr.GetTask(task.ID)
	if updated.Status != TaskStatusCanceled {
		t.Errorf("status = %v, want canceled", updated.Status)
	}
}

// TestTaskManagerShutdownCancelsTasks proves Shutdown tears down in-flight task
// goroutines instead of leaking them.
func TestTaskManagerShutdownCancelsTasks(t *testing.T) {
	mgr := NewTaskManager()

	started := make(chan struct{})
	done := make(chan struct{})
	mgr.RegisterTask("blocker", TaskSpec{
		Name: "blocker",
		Handler: func(ctx context.Context, _ map[string]any) (*TaskResult, error) {
			close(started)
			<-ctx.Done()
			close(done)
			return nil, ctx.Err()
		},
	})

	if _, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "blocker"}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	<-started
	mgr.Shutdown()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not cancel the running task's context")
	}
}

// TestTaskRetentionEviction proves terminal tasks are evicted after the
// retention window, bounding the registry.
func TestTaskRetentionEviction(t *testing.T) {
	mgr := NewTaskManager(WithTaskRetention(10 * time.Millisecond))

	mgr.RegisterTask("quick", TaskSpec{
		Name: "quick",
		Handler: func(_ context.Context, _ map[string]any) (*TaskResult, error) {
			return &TaskResult{Data: "ok"}, nil
		},
	})

	first, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "quick"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// Wait for the first task to reach a terminal state (so CompletedAt is set).
	waitForStatus(t, mgr, first.ID, TaskStatusCompleted)

	// Wait past the retention window, then a new CreateTask triggers a sweep.
	time.Sleep(30 * time.Millisecond)
	if _, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "quick"}); err != nil {
		t.Fatalf("second CreateTask() error = %v", err)
	}

	if _, err := mgr.GetTask(first.ID); err == nil {
		t.Error("expected the first (expired) task to be evicted, but it is still present")
	}
}

// TestTaskRegistryCapRejects proves creation is rejected once the cap is full
// of non-terminal tasks, so the map can never grow without bound.
func TestTaskRegistryCapRejects(t *testing.T) {
	mgr := NewTaskManager(WithMaxTasks(1), WithTaskRetention(0))
	defer mgr.Shutdown()

	started := make(chan struct{}, 1)
	mgr.RegisterTask("hold", TaskSpec{
		Name: "hold",
		Handler: func(ctx context.Context, _ map[string]any) (*TaskResult, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	if _, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "hold"}); err != nil {
		t.Fatalf("first CreateTask() error = %v", err)
	}
	<-started // ensure the task is running (non-terminal) and occupies the cap

	if _, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "hold"}); err == nil {
		t.Error("expected CreateTask to be rejected when the registry is full of active tasks")
	}
}

// TestListTasksCursorPaging proves ListTasks returns a deterministic order and
// honours the cursor with a Next token across pages.
func TestListTasksCursorPaging(t *testing.T) {
	mgr := NewTaskManager()
	mgr.RegisterTask("t", TaskSpec{
		Name: "t",
		Handler: func(_ context.Context, _ map[string]any) (*TaskResult, error) {
			return &TaskResult{}, nil
		},
	})

	const total = 5
	for i := 0; i < total; i++ {
		if _, err := mgr.CreateTask(context.Background(), CreateTaskRequest{Name: "t"}); err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		time.Sleep(time.Millisecond) // distinct CreatedAt for a stable order
	}

	seen := make(map[string]bool)
	cursor := ""
	pages := 0
	for {
		resp, err := mgr.ListTasks(2, cursor)
		if err != nil {
			t.Fatalf("ListTasks() error = %v", err)
		}
		pages++
		for _, task := range resp.Tasks {
			if seen[task.ID] {
				t.Fatalf("task %s returned on more than one page", task.ID)
			}
			seen[task.ID] = true
		}
		if resp.Next == "" {
			break
		}
		cursor = resp.Next
		if pages > total+2 {
			t.Fatal("cursor paging did not terminate")
		}
	}

	if len(seen) != total {
		t.Errorf("paged over %d tasks, want %d", len(seen), total)
	}
}

func waitForStatus(t *testing.T, mgr *TaskManager, id string, want TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := mgr.GetTask(id)
		if err == nil && task.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %v", id, want)
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
