package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

// Sentinel errors from the augmented-task registry, mapped to JSON-RPC
// -32602 (Invalid params) by the dispatcher per the spec.
var (
	// ErrAugTaskNotFound is returned for an unknown or expired taskId.
	ErrAugTaskNotFound = errors.New("task not found")
	// ErrAugTaskTerminal is returned when cancelling an already-terminal task.
	ErrAugTaskTerminal = errors.New("task already in terminal status")
)

// This file implements task-augmented requests per MCP 2025-11-25 (SEP-1686):
// a tools/call carrying a `task` field returns a CreateTaskResult immediately
// and executes in the background; the requestor then polls tasks/get and
// retrieves the result via tasks/result. It is intentionally separate from the
// legacy TaskManager in tasks.go (a different, pre-spec model) — the spec model
// is the one wired into the dispatcher.

// AugTaskStatus is the lifecycle state of a task-augmented request. The valid
// transitions are working→{input_required,completed,failed,cancelled} and
// input_required→{working,completed,failed,cancelled}; the last three are
// terminal.
type AugTaskStatus string

const (
	AugTaskWorking       AugTaskStatus = "working"
	AugTaskInputRequired AugTaskStatus = "input_required"
	AugTaskCompleted     AugTaskStatus = "completed"
	AugTaskFailed        AugTaskStatus = "failed"
	AugTaskCancelled     AugTaskStatus = "cancelled"
)

func (s AugTaskStatus) terminal() bool {
	return s == AugTaskCompleted || s == AugTaskFailed || s == AugTaskCancelled
}

// TaskSupport declares whether a tool may (or must) be invoked as a task, as
// advertised via a tool's `execution.taskSupport` in tools/list.
type TaskSupport string

const (
	TaskSupportForbidden TaskSupport = "forbidden"
	TaskSupportOptional  TaskSupport = "optional"
	TaskSupportRequired  TaskSupport = "required"
)

// AugTask is the spec Task object (2025-11-25). Times are RFC 3339 strings on
// the wire; TTL/pollInterval are milliseconds.
type AugTask struct {
	TaskID        string        `json:"taskId"`
	Status        AugTaskStatus `json:"status"`
	StatusMessage string        `json:"statusMessage,omitempty"`
	CreatedAt     string        `json:"createdAt"`
	LastUpdatedAt string        `json:"lastUpdatedAt"`
	TTL           *int64        `json:"ttl"`
	PollInterval  *int64        `json:"pollInterval,omitempty"`

	// internal (never serialized): the underlying request result once terminal
	// (execResult, or execErr for a protocol-level error), a done channel for
	// tasks/result blocking, and a cancel func for the background execution.
	execResult any
	execErr    error
	done       chan struct{}
	cancel     context.CancelFunc
	ttlDur     time.Duration
	expireAt   time.Time
}

// augTaskRegistry is a bounded, TTL-evicting store of task-augmented requests.
type augTaskRegistry struct {
	mu           sync.Mutex
	tasks        map[string]*AugTask
	maxTasks     int
	pollInterval int64 // ms, suggested to requestors
	now          func() time.Time
}

func newAugTaskRegistry() *augTaskRegistry {
	return &augTaskRegistry{
		tasks:        make(map[string]*AugTask),
		maxTasks:     defaultMaxTasks,
		pollInterval: 1000,
		now:          time.Now,
	}
}

// newAugTaskID returns a cryptographically secure task id (spec: unguessable
// when context-binding is unavailable).
func newAugTaskID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// create registers a new working task with the requested ttl (nil = unlimited).
// It evicts expired tasks first and enforces the size cap.
func (r *augTaskRegistry) create(ttlMs *int64) (*AugTask, error) {
	id, err := newAugTaskID()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	if len(r.tasks) >= r.maxTasks {
		r.evictOldestTerminalLocked()
	}
	now := r.now()
	ts := now.UTC().Format(time.RFC3339)
	poll := r.pollInterval
	t := &AugTask{
		TaskID:        id,
		Status:        AugTaskWorking,
		CreatedAt:     ts,
		LastUpdatedAt: ts,
		TTL:           ttlMs,
		PollInterval:  &poll,
		done:          make(chan struct{}),
	}
	if ttlMs != nil {
		t.ttlDur = time.Duration(*ttlMs) * time.Millisecond
		t.expireAt = now.Add(t.ttlDur)
	}
	r.tasks[id] = t
	return t, nil
}

// updateTTL refreshes a non-terminal task's ttl (and expiry), returning the
// updated task. A nil ttl clears the deadline (unlimited). It returns
// ErrAugTaskNotFound for an unknown/expired id and ErrAugTaskTerminal for a task
// that has already finished (its ttl can no longer be meaningfully extended).
func (r *augTaskRegistry) updateTTL(id string, ttlMs *int64) (*AugTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || r.expiredLocked(t) {
		if ok {
			delete(r.tasks, id)
		}
		return nil, ErrAugTaskNotFound
	}
	if t.Status.terminal() {
		return nil, ErrAugTaskTerminal
	}
	now := r.now()
	t.TTL = ttlMs
	if ttlMs != nil {
		t.ttlDur = time.Duration(*ttlMs) * time.Millisecond
		t.expireAt = now.Add(t.ttlDur)
	} else {
		t.ttlDur = 0
		t.expireAt = time.Time{}
	}
	t.LastUpdatedAt = now.UTC().Format(time.RFC3339)
	return t, nil
}

// get returns a live (non-expired) task by id.
func (r *augTaskRegistry) get(id string) (*AugTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, false
	}
	if r.expiredLocked(t) {
		delete(r.tasks, id)
		return nil, false
	}
	return t, true
}

// complete moves a task to a terminal status carrying the underlying result
// (execResult) or a protocol-level error (execErr), and closes its done channel
// exactly once. A no-op if the task is already terminal (e.g. cancelled).
func (r *augTaskRegistry) complete(id string, status AugTaskStatus, result any, execErr error, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || t.Status.terminal() {
		return
	}
	t.Status = status
	t.StatusMessage = msg
	t.execResult = result
	t.execErr = execErr
	t.LastUpdatedAt = r.now().UTC().Format(time.RFC3339)
	close(t.done)
}

// cancelTask transitions a task to cancelled (best-effort stopping its
// execution). It returns ErrAugTaskNotFound for an unknown id and
// ErrAugTaskTerminal if the task already reached a terminal status.
func (r *augTaskRegistry) cancelTask(id string) (*AugTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || r.expiredLocked(t) {
		return nil, ErrAugTaskNotFound
	}
	if t.Status.terminal() {
		return nil, ErrAugTaskTerminal
	}
	t.Status = AugTaskCancelled
	t.StatusMessage = "The task was cancelled by request."
	t.LastUpdatedAt = r.now().UTC().Format(time.RFC3339)
	if t.cancel != nil {
		t.cancel()
	}
	close(t.done)
	return t, nil
}

// list returns tasks sorted newest-first with cursor pagination.
func (r *augTaskRegistry) list(cursor string, limit int) ([]*AugTask, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	all := make([]*AugTask, 0, len(r.tasks))
	for _, t := range r.tasks {
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })

	start := 0
	if cursor != "" {
		for i, t := range all {
			if t.TaskID == cursor {
				start = i + 1
				break
			}
		}
	}
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}
	end := start + limit
	next := ""
	if end < len(all) {
		next = all[end-1].TaskID
	} else {
		end = len(all)
	}
	return all[start:end], next
}

// StartAugmentedCall creates a working task and runs exec in the background,
// recording the outcome so tasks/result can return it. exec returns the
// underlying result (e.g. a CallToolResult map), whether that result represents
// a tool execution error (isError), and any protocol-level error. The returned
// AugTask is the CreateTaskResult payload sent to the requestor immediately.
func (s *Server) StartAugmentedCall(ctx context.Context, ttlMs *int64, exec func(context.Context) (result any, isError bool, err error)) (*AugTask, error) {
	t, err := s.augTasks.create(ttlMs)
	if err != nil {
		return nil, err
	}
	// A cancellable context so tasks/cancel can stop the background work.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.augTasks.mu.Lock()
	t.cancel = cancel
	s.augTasks.mu.Unlock()

	go func() {
		defer cancel()
		result, isError, execErr := exec(runCtx)
		switch {
		case execErr != nil:
			s.augTasks.complete(t.TaskID, AugTaskFailed, nil, execErr, execErr.Error())
		case isError:
			s.augTasks.complete(t.TaskID, AugTaskFailed, result, nil, "tool execution error")
		default:
			s.augTasks.complete(t.TaskID, AugTaskCompleted, result, nil, "")
		}
	}()
	return t, nil
}

// GetAugTask returns a task's current state by id (nil, false if unknown/expired).
func (s *Server) GetAugTask(id string) (*AugTask, bool) { return s.augTasks.get(id) }

// AwaitAugTaskResult blocks until the task reaches a terminal status (or ctx is
// cancelled), then returns its underlying result and protocol error. It returns
// ErrAugTaskNotFound for an unknown/expired id.
func (s *Server) AwaitAugTaskResult(ctx context.Context, id string) (result any, execErr error, err error) {
	t, ok := s.augTasks.get(id)
	if !ok {
		return nil, nil, ErrAugTaskNotFound
	}
	select {
	case <-t.done:
		return t.execResult, t.execErr, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// CancelAugTask cancels a task (ErrAugTaskNotFound / ErrAugTaskTerminal on
// failure), returning the updated task.
func (s *Server) CancelAugTask(id string) (*AugTask, error) { return s.augTasks.cancelTask(id) }

// UpdateAugTask refreshes a non-terminal task's ttl (nil clears the deadline),
// returning the updated task. It backs the tasks/update method (MCP 2026-07-28):
// a caller polling a long-running task can extend its lifetime so it is not
// evicted before completion. Errors: ErrAugTaskNotFound (unknown/expired),
// ErrAugTaskTerminal (already finished).
func (s *Server) UpdateAugTask(id string, ttlMs *int64) (*AugTask, error) {
	return s.augTasks.updateTTL(id, ttlMs)
}

// ListAugTasks returns tasks newest-first with cursor pagination.
func (s *Server) ListAugTasks(cursor string, limit int) ([]*AugTask, string) {
	return s.augTasks.list(cursor, limit)
}

func (r *augTaskRegistry) expiredLocked(t *AugTask) bool {
	return t.TTL != nil && !t.expireAt.IsZero() && r.now().After(t.expireAt)
}

func (r *augTaskRegistry) evictExpiredLocked() {
	for id, t := range r.tasks {
		if r.expiredLocked(t) {
			delete(r.tasks, id)
		}
	}
}

// evictOldestTerminalLocked drops the oldest terminal task to make room; if none
// are terminal the cap is a soft ceiling (working tasks are never evicted).
func (r *augTaskRegistry) evictOldestTerminalLocked() {
	var oldestID string
	var oldest string
	for id, t := range r.tasks {
		if !t.Status.terminal() {
			continue
		}
		if oldestID == "" || t.CreatedAt < oldest {
			oldestID, oldest = id, t.CreatedAt
		}
	}
	if oldestID != "" {
		delete(r.tasks, oldestID)
	}
}
