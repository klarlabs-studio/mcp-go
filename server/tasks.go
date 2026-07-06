package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// Task registry bounds. The registry is an in-memory map that, left
// unbounded, is a denial-of-service vector: every CreateTask inserts forever.
// These defaults cap retention so finished tasks are evicted and the map size
// stays bounded regardless of client behavior.
const (
	defaultMaxTasks      = 1000
	defaultTaskRetention = 10 * time.Minute
	defaultListLimit     = 100
	maxListLimit         = 100
)

// isTerminal reports whether a task has reached a final state and will not
// transition again.
func isTerminal(s TaskStatus) bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusCanceled
}

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

// Task metadata field keys.
const (
	metaKeyName   = "name"
	metaKeyParams = "params"
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
	// cancels holds the context-cancel func for each in-flight task so
	// CancelTask can actually stop the running goroutine (not just flip a
	// status flag). Guarded by mu alongside tasks.
	cancels   map[string]context.CancelFunc
	maxTasks  int
	retention time.Duration
}

func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		tasks:     make(map[string]*Task),
		handlers:  make(map[string]TaskSpec),
		cancels:   make(map[string]context.CancelFunc),
		maxTasks:  defaultMaxTasks,
		retention: defaultTaskRetention,
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

// CreateTask stores a task and the cancel func for its running goroutine.
// Before inserting it evicts expired terminal tasks and, if still at capacity,
// evicts the oldest terminal task; if the registry is full of non-terminal
// tasks it rejects the insert so the map can never grow without bound.
func (r *taskRegistry) CreateTask(task *Task, cancel context.CancelFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.evictExpiredLocked(time.Now())

	if len(r.tasks) >= r.maxTasks && !r.evictOneTerminalLocked() {
		return fmt.Errorf("task registry full: %d active tasks", len(r.tasks))
	}

	r.tasks[task.ID] = task
	r.cancels[task.ID] = cancel
	return nil
}

// getCancel returns the cancel func registered for a task, if any.
func (r *taskRegistry) getCancel(id string) (context.CancelFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cancel, ok := r.cancels[id]
	return cancel, ok
}

// evictExpiredLocked removes terminal tasks whose completion is older than the
// retention window. Caller must hold r.mu.
func (r *taskRegistry) evictExpiredLocked(now time.Time) {
	if r.retention <= 0 {
		return
	}
	for id, t := range r.tasks {
		if t.CompletedAt != nil && now.Sub(*t.CompletedAt) >= r.retention {
			delete(r.tasks, id)
			delete(r.cancels, id)
		}
	}
}

// evictOneTerminalLocked removes the oldest terminal task and reports whether
// one was evicted. Caller must hold r.mu.
func (r *taskRegistry) evictOneTerminalLocked() bool {
	var oldestID string
	var oldest time.Time
	for id, t := range r.tasks {
		if !isTerminal(t.Status) {
			continue
		}
		if oldestID == "" || t.CreatedAt.Before(oldest) {
			oldestID = id
			oldest = t.CreatedAt
		}
	}
	if oldestID == "" {
		return false
	}
	delete(r.tasks, oldestID)
	delete(r.cancels, oldestID)
	return true
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
	delete(r.cancels, id)
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

// TaskManagerOption configures a TaskManager's registry bounds.
type TaskManagerOption func(*taskRegistry)

// WithMaxTasks caps how many tasks the registry retains. When the cap is
// reached, terminal tasks are evicted oldest-first; if none are terminal,
// CreateTask is rejected. Values <= 0 keep the default.
func WithMaxTasks(n int) TaskManagerOption {
	return func(r *taskRegistry) {
		if n > 0 {
			r.maxTasks = n
		}
	}
}

// WithTaskRetention sets how long a terminal task is kept before eviction.
// A value <= 0 disables time-based eviction (the cap still bounds the map).
func WithTaskRetention(d time.Duration) TaskManagerOption {
	return func(r *taskRegistry) {
		r.retention = d
	}
}

type TaskManager struct {
	registry *taskRegistry
	// ctx is the manager-owned parent for every task goroutine; canceling it
	// (via Shutdown) tears down all in-flight tasks instead of leaking them
	// past server shutdown.
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTaskManager(opts ...TaskManagerOption) *TaskManager {
	registry := newTaskRegistry()
	for _, opt := range opts {
		opt(registry)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskManager{
		registry: registry,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Shutdown cancels every in-flight task goroutine. Safe to call more than once.
func (m *TaskManager) Shutdown() {
	m.cancel()
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
			metaKeyName:   req.Name,
			metaKeyParams: req.Params,
		},
	}

	// Derive the task context from the manager-owned context so Shutdown
	// cancels it, and store the cancel func so CancelTask can stop it too.
	taskCtx, cancel := context.WithCancel(m.ctx)

	if err := m.registry.CreateTask(task, cancel); err != nil {
		cancel()
		return nil, err
	}

	taskCopy := *task

	go func() {
		defer cancel()
		m.registry.UpdateTask(task.ID, func(t *Task) {
			// A cancel that lands before the goroutine starts running must
			// not be clobbered back to "running".
			if isTerminal(t.Status) {
				return
			}
			t.Status = TaskStatusRunning
			t.Message = "Task running"
		})

		result, err := spec.Handler(taskCtx, req.Params)
		m.registry.UpdateTask(task.ID, func(t *Task) {
			// CancelTask may have already marked the task terminal; its
			// decision wins over whatever the (now-unblocked) handler returned.
			if isTerminal(t.Status) {
				return
			}
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

// ListTasks returns a deterministically ordered, cursor-paged view of the
// registry. Tasks are ordered by CreatedAt (then ID as a stable tie-break) so
// paging is repeatable over the otherwise randomly-ordered map. The cursor is
// the ID of the last task on the previous page; Next is set when more tasks
// remain.
func (m *TaskManager) ListTasks(limit int, cursor string) (*ListTasksResponse, error) {
	tasks := m.registry.ListTasks()

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}

	start := 0
	if cursor != "" {
		for i, t := range tasks {
			if t.ID == cursor {
				start = i + 1
				break
			}
		}
	}
	if start > len(tasks) {
		start = len(tasks)
	}

	end := start + limit
	var next string
	if end < len(tasks) {
		next = tasks[end-1].ID
	} else {
		end = len(tasks)
	}

	return &ListTasksResponse{
		Tasks: tasks[start:end],
		Next:  next,
	}, nil
}

func (m *TaskManager) CancelTask(taskID string) error {
	task, ok := m.registry.GetTask(taskID)
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if isTerminal(task.Status) {
		return fmt.Errorf("task already terminal: %s", task.Status)
	}

	// Mark canceled first so the handler goroutine's completion path (which
	// re-checks isTerminal) won't overwrite the outcome, then cancel the
	// context to actually stop the running goroutine.
	now := time.Now()
	m.registry.UpdateTask(taskID, func(t *Task) {
		if isTerminal(t.Status) {
			return
		}
		t.Status = TaskStatusCanceled
		t.Message = "Task canceled"
		t.CompletedAt = &now
	})

	if cancel, ok := m.registry.getCancel(taskID); ok {
		cancel()
	}

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
	// Guard the lazy init with the Server mutex: RegisterTask (setup) and
	// Tasks() (request path) previously read/assigned s.tasks with no
	// synchronization, a data race under -race.
	s.mu.Lock()
	if s.tasks == nil {
		s.tasks = NewTaskManager()
	}
	tm := s.tasks
	s.mu.Unlock()

	tm.RegisterTask(name, TaskSpec{
		Name:        name,
		Description: description,
		Handler:     handler,
	})
}

func (s *Server) Tasks() *TaskManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks
}
