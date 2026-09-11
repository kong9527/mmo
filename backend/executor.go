package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type mailbox struct {
	results  chan Result
	capacity int64
	reserved atomic.Int64
	closed   atomic.Bool
}

func newMailbox(size int) *mailbox {
	return &mailbox{
		results:  make(chan Result, size),
		capacity: int64(size),
	}
}

func (m *mailbox) reserve() bool {
	if m.closed.Load() {
		return false
	}
	for {
		current := m.reserved.Load()
		if current >= m.capacity {
			return false
		}
		if m.reserved.CompareAndSwap(current, current+1) {
			if m.closed.Load() {
				m.release()
				return false
			}
			return true
		}
	}
}

func (m *mailbox) release() {
	m.reserved.Add(-1)
}

func (m *mailbox) deliver(result Result) bool {
	if m.closed.Load() {
		m.release()
		return false
	}
	select {
	case m.results <- result:
		return true
	default:
		// Every accepted task reserves one slot, so this branch is defensive.
		m.release()
		return false
	}
}

func (m *mailbox) drain(max int) []Result {
	if max <= 0 {
		return nil
	}
	results := make([]Result, 0, max)
	for len(results) < max {
		select {
		case result := <-m.results:
			m.release()
			results = append(results, result)
		default:
			return results
		}
	}
	return results
}

func (m *mailbox) close() {
	m.closed.Store(true)
}

type taskControl struct {
	worldID WorldID
	ctx     context.Context
	cancel  context.CancelFunc
}

type taskEntry struct {
	task    Task
	meta    TaskMeta
	policy  TaskPolicy
	mailbox *mailbox
	control *taskControl
}

type taskOutcome struct {
	data      any
	err       error
	recovered any
}

type Executor struct {
	cfg Config

	ctx    context.Context
	cancel context.CancelFunc
	tasks  chan *taskEntry

	mailboxMu sync.RWMutex
	mailboxes map[WorldID]*mailbox

	controlMu sync.Mutex
	controls  map[TaskID]*taskControl

	nextTaskID atomic.Uint64
	closed     atomic.Bool
	workers    sync.WaitGroup
}

func New(cfg Config) (*Executor, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	executor := &Executor{
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		tasks:     make(chan *taskEntry, cfg.TaskQueueSize),
		mailboxes: make(map[WorldID]*mailbox),
		controls:  make(map[TaskID]*taskControl),
	}
	for i := 0; i < cfg.WorkerCount; i++ {
		executor.workers.Add(1)
		go executor.worker()
	}
	return executor, nil
}

func (e *Executor) RegisterWorld(worldID WorldID, resultQueueSize int) error {
	if e.closed.Load() {
		return ErrClosed
	}
	if worldID == 0 || resultQueueSize <= 0 {
		return fmt.Errorf("%w: invalid world mailbox configuration", ErrInvalidConfig)
	}
	e.mailboxMu.Lock()
	defer e.mailboxMu.Unlock()
	if e.closed.Load() {
		return ErrClosed
	}
	if _, exists := e.mailboxes[worldID]; exists {
		return ErrWorldExists
	}
	e.mailboxes[worldID] = newMailbox(resultQueueSize)
	return nil
}

func (e *Executor) UnregisterWorld(worldID WorldID) {
	e.mailboxMu.Lock()
	mailbox := e.mailboxes[worldID]
	delete(e.mailboxes, worldID)
	e.mailboxMu.Unlock()
	if mailbox != nil {
		mailbox.close()
	}

	e.controlMu.Lock()
	for _, control := range e.controls {
		if control.worldID == worldID {
			control.cancel()
		}
	}
	e.controlMu.Unlock()
}

func (e *Executor) Submit(task Task) (TaskID, error) {
	if task == nil {
		return 0, ErrInvalidTask
	}
	if e.closed.Load() {
		return 0, ErrClosed
	}
	meta := task.Meta()
	if err := meta.validate(); err != nil {
		return 0, err
	}

	e.mailboxMu.RLock()
	mailbox := e.mailboxes[meta.WorldID]
	e.mailboxMu.RUnlock()
	if mailbox == nil {
		return 0, ErrWorldNotFound
	}
	if !mailbox.reserve() {
		return 0, ErrResultQueueFull
	}

	meta.TaskID = TaskID(e.nextTaskID.Add(1))
	ctx, cancel := context.WithCancel(e.ctx)
	control := &taskControl{worldID: meta.WorldID, ctx: ctx, cancel: cancel}
	entry := &taskEntry{
		task:    task,
		meta:    meta,
		policy:  task.Policy().normalized(),
		mailbox: mailbox,
		control: control,
	}

	e.controlMu.Lock()
	e.controls[meta.TaskID] = control
	e.controlMu.Unlock()

	select {
	case <-e.ctx.Done():
		e.releaseEntry(entry)
		return 0, ErrClosed
	case e.tasks <- entry:
		return meta.TaskID, nil
	default:
		e.releaseEntry(entry)
		return 0, ErrTaskQueueFull
	}
}

func (e *Executor) Cancel(taskID TaskID) bool {
	e.controlMu.Lock()
	control := e.controls[taskID]
	e.controlMu.Unlock()
	if control == nil {
		return false
	}
	control.cancel()
	return true
}

func (e *Executor) DrainResults(worldID WorldID, max int) []Result {
	e.mailboxMu.RLock()
	mailbox := e.mailboxes[worldID]
	e.mailboxMu.RUnlock()
	if mailbox == nil {
		return nil
	}
	return mailbox.drain(max)
}

func (e *Executor) Close(ctx context.Context) error {
	if e.closed.CompareAndSwap(false, true) {
		e.cancel()
	}
	done := make(chan struct{})
	go func() {
		e.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		e.cancelQueuedTasks()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Executor) worker() {
	defer e.workers.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case entry := <-e.tasks:
			if entry != nil {
				e.execute(entry)
			}
		}
	}
}

func (e *Executor) execute(entry *taskEntry) {
	startedAt := time.Now()
	outcomes := make(chan taskOutcome, 1)
	go func() {
		outcome := taskOutcome{}
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome.recovered = recovered
			}
			outcomes <- outcome
		}()
		outcome.data, outcome.err = entry.task.Execute(entry.control.ctx)
	}()

	timeout := entry.policy.Timeout
	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}

	result := Result{Meta: entry.meta, StartedAt: startedAt}
	select {
	case outcome := <-outcomes:
		result.Data = outcome.data
		result.Err = outcome.err
		switch {
		case outcome.recovered != nil:
			result.Status = ResultPanic
			result.Err = fmt.Errorf("backend task panic: %v", outcome.recovered)
		case errors.Is(outcome.err, context.Canceled):
			result.Status = ResultCanceled
		case outcome.err != nil:
			result.Status = ResultFailed
		default:
			result.Status = ResultSuccess
		}
	case <-timeoutCh:
		result.Status = entry.policy.TimeoutStatus
		result.Err = context.DeadlineExceeded
		if entry.policy.CancelOnTimeout {
			entry.control.cancel()
		}
		e.finish(entry, result)
		// The timeout result is terminal, but the worker slot remains occupied
		// until Execute actually returns. This keeps live executions bounded even
		// when a task ignores cooperative cancellation.
		select {
		case <-outcomes:
		case <-e.ctx.Done():
		}
		return
	case <-entry.control.ctx.Done():
		result.Status = ResultCanceled
		result.Err = entry.control.ctx.Err()
		e.finish(entry, result)
		// Cancellation is cooperative. Keep the worker occupied until Execute
		// returns so ignored cancellation cannot exceed the configured pool size.
		select {
		case <-outcomes:
		case <-e.ctx.Done():
		}
		return
	}
	e.finish(entry, result)
}

func (e *Executor) finish(entry *taskEntry, result Result) {
	result.FinishedAt = time.Now()
	entry.control.cancel()
	e.controlMu.Lock()
	delete(e.controls, entry.meta.TaskID)
	e.controlMu.Unlock()
	entry.mailbox.deliver(result)
}

func (e *Executor) releaseEntry(entry *taskEntry) {
	entry.control.cancel()
	e.controlMu.Lock()
	delete(e.controls, entry.meta.TaskID)
	e.controlMu.Unlock()
	entry.mailbox.release()
}

func (e *Executor) cancelQueuedTasks() {
	for {
		select {
		case entry := <-e.tasks:
			if entry != nil {
				now := time.Now()
				e.finish(entry, Result{
					Meta:       entry.meta,
					Status:     ResultCanceled,
					Err:        context.Canceled,
					StartedAt:  now,
					FinishedAt: now,
				})
			}
		default:
			return
		}
	}
}

var _ Service = (*Executor)(nil)
