package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

type TaskResult struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type Task struct {
	ID            string         `json:"id"`
	Status        TaskStatus     `json:"status"`
	ProgressToken string         `json:"progressToken,omitempty"`
	Message       string         `json:"message,omitempty"`
	Data          any            `json:"data,omitempty"`
	Result        *TaskResult    `json:"result,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	CompletedAt   *time.Time     `json:"completedAt,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type TaskHandler func(ctx context.Context, input map[string]any) (*TaskResult, error)

type TaskSpec struct {
	Name        string
	Description string
	Handler     TaskHandler
}

type taskRegistry struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	handlers map[string]TaskSpec
}

func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		tasks:    make(map[string]*Task),
		handlers: make(map[string]TaskSpec),
	}
}

func (r *taskRegistry) Register(name string, spec TaskSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = spec
}

func (r *taskRegistry) GetHandler(name string) (TaskSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.handlers[name]
	return spec, ok
}

func (r *taskRegistry) ListHandlers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	return names
}

func (r *taskRegistry) CreateTask(task *Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[task.ID] = task
}

func (r *taskRegistry) GetTask(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	if !ok {
		return nil, false
	}
	taskCopy := *task
	return &taskCopy, true
}

func (r *taskRegistry) UpdateTask(id string, update func(*Task)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok {
		return false
	}
	update(task)
	return true
}

func (r *taskRegistry) ListTasks() []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tasks := make([]*Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		taskCopy := *task
		tasks = append(tasks, &taskCopy)
	}
	return tasks
}

func (r *taskRegistry) DeleteTask(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[id]; !ok {
		return false
	}
	delete(r.tasks, id)
	return true
}

type CreateTaskRequest struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

type CreateTaskResponse struct {
	Task          *Task  `json:"task"`
	ProgressToken string `json:"progressToken,omitempty"`
}

type GetTaskStatusRequest struct {
	TaskID string `json:"taskId"`
}

type ListTasksRequest struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type ListTasksResponse struct {
	Tasks []*Task `json:"tasks"`
	Next  string  `json:"next,omitempty"`
}

type CancelTaskRequest struct {
	TaskID string `json:"taskId"`
}

type CompleteTaskRequest struct {
	TaskID string      `json:"taskId"`
	Result *TaskResult `json:"result"`
}

type TaskResultRequest struct {
	TaskID string `json:"taskId"`
}

type TaskManager struct {
	registry *taskRegistry
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		registry: newTaskRegistry(),
	}
}

func (m *TaskManager) RegisterTask(name string, spec TaskSpec) {
	m.registry.Register(name, spec)
}

func (m *TaskManager) ListTaskNames() []string {
	return m.registry.ListHandlers()
}

func (m *TaskManager) CreateTask(ctx context.Context, req CreateTaskRequest) (*Task, error) {
	spec, ok := m.registry.GetHandler(req.Name)
	if !ok {
		return nil, fmt.Errorf("unknown task: %s", req.Name)
	}

	task := &Task{
		ID:        fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Status:    TaskStatusQueued,
		Message:   "Task queued",
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			"name":   req.Name,
			"params": req.Params,
		},
	}

	m.registry.CreateTask(task)

	taskCopy := *task

	taskCtx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		m.registry.UpdateTask(task.ID, func(t *Task) {
			t.Status = TaskStatusRunning
			t.Message = "Task running"
		})

		result, err := spec.Handler(taskCtx, req.Params)
		m.registry.UpdateTask(task.ID, func(t *Task) {
			if err != nil {
				t.Status = TaskStatusFailed
				t.Message = err.Error()
				t.Result = &TaskResult{Error: err.Error()}
			} else {
				t.Status = TaskStatusCompleted
				t.Message = "Task completed"
				t.Result = result
			}
			now := time.Now()
			t.CompletedAt = &now
		})
	}()

	return &taskCopy, nil
}

func (m *TaskManager) GetTask(taskID string) (*Task, error) {
	task, ok := m.registry.GetTask(taskID)
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	return task, nil
}

func (m *TaskManager) ListTasks(limit int, cursor string) (*ListTasksResponse, error) {
	tasks := m.registry.ListTasks()

	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	if len(tasks) > limit {
		tasks = tasks[:limit]
	}

	return &ListTasksResponse{
		Tasks: tasks,
	}, nil
}

func (m *TaskManager) CancelTask(taskID string) error {
	task, ok := m.registry.GetTask(taskID)
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed || task.Status == TaskStatusCanceled {
		return fmt.Errorf("task already terminal: %s", task.Status)
	}

	now := time.Now()
	m.registry.UpdateTask(taskID, func(t *Task) {
		t.Status = TaskStatusCanceled
		t.Message = "Task canceled"
		t.CompletedAt = &now
	})

	return nil
}

func (m *TaskManager) HandleTaskRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "tasks/create":
		var req CreateTaskRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		return m.CreateTask(ctx, req)

	case "tasks/get":
		var req GetTaskStatusRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		return m.GetTask(req.TaskID)

	case "tasks/list":
		var req ListTasksRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		return m.ListTasks(req.Limit, req.Cursor)

	case "tasks/cancel":
		var req CancelTaskRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := m.CancelTask(req.TaskID); err != nil {
			return nil, err
		}
		return map[string]bool{"canceled": true}, nil

	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

func (m *TaskManager) HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	result, err := m.HandleTaskRequest(ctx, req.Method, req.Params)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error())), nil
	}
	return protocol.NewResponse(req.ID, result), nil
}

func (s *Server) RegisterTask(name string, description string, handler TaskHandler) {
	if s.tasks == nil {
		s.tasks = NewTaskManager()
	}
	s.tasks.RegisterTask(name, TaskSpec{
		Name:        name,
		Description: description,
		Handler:     handler,
	})
}

func (s *Server) Tasks() *TaskManager {
	return s.tasks
}
