package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"go.klarlabs.de/mcp/protocol"
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

const (
	jsonKeyCode    = "code"
	jsonKeyMessage = "message"
)

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

	// Task-level MRTR (SEP-2663): replay the handler after tasks/update supplies
	// inputResponses. pending is the outstanding inputRequests while status is
	// input_required; responses accumulate over the task lifetime.
	exec      func(context.Context) (any, bool, error)
	runCtx    context.Context
	pending   []InputRequest
	responses []InputResponse
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
	return t.snapshot(), true
}

// snapshot copies exported task fields (and the execution outcome) so callers
// can read a task without racing complete()/cancel. The copy shares the done
// channel so Await can wait; cancel is not exposed.
func (t *AugTask) snapshot() *AugTask {
	c := *t
	if t.TTL != nil {
		v := *t.TTL
		c.TTL = &v
	}
	if t.PollInterval != nil {
		v := *t.PollInterval
		c.PollInterval = &v
	}
	c.cancel = nil
	c.exec = nil
	c.runCtx = nil
	if t.pending != nil {
		c.pending = append([]InputRequest(nil), t.pending...)
	}
	if t.responses != nil {
		c.responses = append([]InputResponse(nil), t.responses...)
	}
	return &c
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
	return t.snapshot(), nil
}

// list returns tasks sorted newest-first with cursor pagination.
func (r *augTaskRegistry) list(cursor string, limit int) ([]*AugTask, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	all := make([]*AugTask, 0, len(r.tasks))
	for _, t := range r.tasks {
		all = append(all, t.snapshot())
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
// If exec returns ErrInputRequired, the task pauses at input_required until
// ApplyAugTaskInput supplies matching inputResponses and replays exec.
func (s *Server) StartAugmentedCall(ctx context.Context, ttlMs *int64, exec func(context.Context) (result any, isError bool, err error)) (*AugTask, error) {
	t, err := s.augTasks.create(ttlMs)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.augTasks.mu.Lock()
	t.cancel = cancel
	t.exec = exec
	t.runCtx = runCtx
	snap := t.snapshot()
	s.augTasks.mu.Unlock()

	go s.runAugmented(t.TaskID)
	return snap, nil
}

func (s *Server) runAugmented(id string) {
	s.augTasks.mu.Lock()
	t, ok := s.augTasks.tasks[id]
	if !ok || t.Status.terminal() {
		s.augTasks.mu.Unlock()
		return
	}
	exec := t.exec
	runCtx := t.runCtx
	responses := append([]InputResponse(nil), t.responses...)
	s.augTasks.mu.Unlock()
	if exec == nil || runCtx == nil {
		s.augTasks.complete(id, AugTaskFailed, nil, errors.New("task has no executor"), "task has no executor")
		s.taskSubs.notify(s.mustGet(id))
		return
	}

	if sess := SessionFromContext(runCtx); sess != nil {
		forked := sess.ForkWithBroker(NewInputBroker(responses, nil))
		runCtx = ContextWithSession(runCtx, forked)
		if forked.SupportsFeature("elicitation") {
			runCtx = ContextWithElicitor(runCtx, NewElicitor(forked))
		}
		if forked.SupportsFeature("channels") {
			runCtx = ContextWithChannel(runCtx, NewChannelSender(forked))
		}
	}

	result, isError, execErr := exec(runCtx)
	if errors.Is(execErr, ErrInputRequired) {
		var pending []InputRequest
		if sess := SessionFromContext(runCtx); sess != nil {
			pending = sess.InputBroker().Pending()
		}
		s.augTasks.pause(id, pending)
		s.taskSubs.notify(s.mustGet(id))
		return
	}
	switch {
	case execErr != nil:
		s.augTasks.complete(id, AugTaskFailed, nil, execErr, execErr.Error())
	case isError:
		s.augTasks.complete(id, AugTaskCompleted, result, nil, "")
	default:
		s.augTasks.complete(id, AugTaskCompleted, result, nil, "")
	}
	s.taskSubs.notify(s.mustGet(id))
}

func (s *Server) mustGet(id string) *AugTask {
	t, _ := s.augTasks.get(id)
	return t
}

func (r *augTaskRegistry) pause(id string, pending []InputRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || t.Status.terminal() {
		return
	}
	t.Status = AugTaskInputRequired
	t.pending = pending
	t.LastUpdatedAt = r.now().UTC().Format(time.RFC3339)
}

// GetAugTask returns a task's current state by id (nil, false if unknown/expired).
func (s *Server) GetAugTask(id string) (*AugTask, bool) { return s.augTasks.get(id) }

// Outcome returns the underlying request result and any protocol-level error
// recorded when the task reached a terminal status. Both are nil while the
// task is still running. Used by modern tasks/get to inline completed/failed
// payloads (SEP-2663).
func (t *AugTask) Outcome() (result any, execErr error) {
	if t == nil {
		return nil, nil
	}
	return t.execResult, t.execErr
}

// Pending returns outstanding inputRequests while the task is input_required.
func (t *AugTask) Pending() []InputRequest {
	if t == nil {
		return nil
	}
	return t.pending
}

// AwaitAugTaskResult blocks until the task reaches a terminal status (or ctx is
// cancelled), then returns its underlying result and protocol error. It returns
// ErrAugTaskNotFound for an unknown/expired id.
func (s *Server) AwaitAugTaskResult(ctx context.Context, id string) (result any, execErr error, err error) {
	return s.augTasks.await(ctx, id)
}

func (r *augTaskRegistry) await(ctx context.Context, id string) (any, error, error) {
	r.mu.Lock()
	t, ok := r.tasks[id]
	if !ok || r.expiredLocked(t) {
		if ok {
			delete(r.tasks, id)
		}
		r.mu.Unlock()
		return nil, nil, ErrAugTaskNotFound
	}
	done := t.done
	r.mu.Unlock()

	select {
	case <-done:
		r.mu.Lock()
		defer r.mu.Unlock()
		t, ok := r.tasks[id]
		if !ok {
			return nil, nil, ErrAugTaskNotFound
		}
		return t.execResult, t.execErr, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// CancelAugTask cancels a task (ErrAugTaskNotFound / ErrAugTaskTerminal on
// failure), returning the updated task.
func (s *Server) CancelAugTask(id string) (*AugTask, error) {
	t, err := s.augTasks.cancelTask(id)
	if err == nil {
		s.taskSubs.notify(t)
	}
	return t, err
}

// UpdateAugTask refreshes a non-terminal task's ttl. A nil ttl leaves the
// existing deadline unchanged. Errors: ErrAugTaskNotFound, ErrAugTaskTerminal.
func (s *Server) UpdateAugTask(id string, ttlMs *int64) (*AugTask, error) {
	t, _, err := s.augTasks.applyInput(id, nil, ttlMs)
	return t, err
}

// ApplyAugTaskInput records inputResponses from tasks/update (SEP-2663). Unknown
// or already-satisfied keys are ignored. When the task is input_required and at
// least one outstanding request is fulfilled, the handler is replayed.
func (s *Server) ApplyAugTaskInput(id string, responses []InputResponse, ttlMs *int64) (*AugTask, error) {
	t, resume, err := s.augTasks.applyInput(id, responses, ttlMs)
	if err != nil {
		return nil, err
	}
	if resume {
		go s.runAugmented(id)
	}
	s.taskSubs.notify(s.mustGet(id))
	return t, nil
}

func (r *augTaskRegistry) applyInput(id string, incoming []InputResponse, ttlMs *int64) (*AugTask, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || r.expiredLocked(t) {
		if ok {
			delete(r.tasks, id)
		}
		return nil, false, ErrAugTaskNotFound
	}
	if t.Status.terminal() {
		return nil, false, ErrAugTaskTerminal
	}
	outstanding := make(map[string]struct{}, len(t.pending))
	for _, p := range t.pending {
		outstanding[p.ID] = struct{}{}
	}
	have := make(map[string]struct{}, len(t.responses))
	for _, resp := range t.responses {
		have[resp.ID] = struct{}{}
	}
	accepted := 0
	for _, in := range incoming {
		if in.ID == "" {
			continue
		}
		if _, ok := outstanding[in.ID]; !ok {
			continue
		}
		if _, ok := have[in.ID]; ok {
			continue
		}
		t.responses = append(t.responses, in)
		have[in.ID] = struct{}{}
		accepted++
	}
	if ttlMs != nil {
		now := r.now()
		t.TTL = ttlMs
		t.ttlDur = time.Duration(*ttlMs) * time.Millisecond
		t.expireAt = now.Add(t.ttlDur)
		t.LastUpdatedAt = now.UTC().Format(time.RFC3339)
	}
	resume := t.Status == AugTaskInputRequired && accepted > 0
	if resume {
		t.Status = AugTaskWorking
		t.pending = nil
		t.LastUpdatedAt = r.now().UTC().Format(time.RFC3339)
	}
	return t.snapshot(), resume, nil
}

// TaskNotificationParams is the 2026-07-28 DetailedTask object used by
// tasks/get and notifications/tasks (resultType "complete").
func TaskNotificationParams(t *AugTask) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	m := map[string]any{
		"taskId":        t.TaskID,
		"status":        string(t.Status),
		"createdAt":     t.CreatedAt,
		"lastUpdatedAt": t.LastUpdatedAt,
		"ttlMs":         t.TTL,
		"resultType":    protocol.ResultTypeComplete,
	}
	if t.StatusMessage != "" {
		m["statusMessage"] = t.StatusMessage
	}
	if t.PollInterval != nil {
		m["pollIntervalMs"] = *t.PollInterval
	}
	switch t.Status {
	case AugTaskInputRequired:
		m["inputRequests"] = inputRequestsWire(t.pending)
	case AugTaskCompleted:
		if t.execResult != nil {
			m["result"] = t.execResult
		}
	case AugTaskFailed:
		m["error"] = taskErrorWire(t.execErr)
	}
	return m
}

// inputRequestsWire encodes outstanding requests as the spec's InputRequests
// map: each request ID keys its {"method", "params"} request object.
func inputRequestsWire(reqs []InputRequest) map[string]InputRequest {
	out := make(map[string]InputRequest, len(reqs))
	for _, r := range reqs {
		out[r.ID] = r
	}
	return out
}

func taskErrorWire(err error) map[string]any {
	if err == nil {
		return map[string]any{jsonKeyCode: protocol.CodeInternalError, jsonKeyMessage: "task failed"}
	}
	var mcpErr *protocol.Error
	if errors.As(err, &mcpErr) {
		out := map[string]any{jsonKeyCode: mcpErr.Code, jsonKeyMessage: mcpErr.Message}
		if mcpErr.Data != nil {
			out["data"] = mcpErr.Data
		}
		return out
	}
	return map[string]any{jsonKeyCode: protocol.CodeInternalError, jsonKeyMessage: err.Error()}
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
