package backend

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testTask struct {
	meta   TaskMeta
	policy TaskPolicy
	run    func(context.Context) (any, error)
}

func (t testTask) Meta() TaskMeta       { return t.meta }
func (t testTask) Policy() TaskPolicy   { return t.policy }
func (t testTask) Execute(ctx context.Context) (any, error) {
	return t.run(ctx)
}

func newTestExecutor(t *testing.T, workers, queueSize int) *Executor {
	t.Helper()
	executor, err := New(Config{WorkerCount: workers, TaskQueueSize: queueSize})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := executor.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return executor
}

func waitResult(t *testing.T, executor *Executor, worldID WorldID) Result {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if results := executor.DrainResults(worldID, 1); len(results) == 1 {
			return results[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for result")
	return Result{}
}

func taskMeta(worldID WorldID) TaskMeta {
	return TaskMeta{
		Kind:        TaskKind(1),
		WorldID:     worldID,
		RoomID:      "room-1",
		EntityID:    EntityID(10),
		EntityEpoch: 3,
		SubmitTick:  7,
	}
}

func TestExecutorDeliversOneSuccessfulResultToReservedWorldMailbox(t *testing.T) {
	executor := newTestExecutor(t, 1, 4)
	if err := executor.RegisterWorld(1, 1); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}

	taskID, err := executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			return "profile-loaded", nil
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	result := waitResult(t, executor, 1)
	if result.Meta.TaskID != taskID || result.Status != ResultSuccess {
		t.Fatalf("result = %#v, want task %d success", result, taskID)
	}
	if result.Data != "profile-loaded" || result.Meta.SubmitTick != 7 {
		t.Fatalf("result payload/meta = %#v", result)
	}
	if got := executor.DrainResults(1, 1); len(got) != 0 {
		t.Fatalf("duplicate result = %#v", got)
	}
}

func TestSubmitReservesMailboxCapacity(t *testing.T) {
	executor := newTestExecutor(t, 1, 4)
	if err := executor.RegisterWorld(1, 1); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	release := make(chan struct{})
	first := testTask{meta: taskMeta(1), run: func(context.Context) (any, error) {
		<-release
		return nil, nil
	}}
	if _, err := executor.Submit(first); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	if _, err := executor.Submit(first); !errors.Is(err, ErrResultQueueFull) {
		t.Fatalf("second Submit() error = %v, want ErrResultQueueFull", err)
	}
	close(release)
	_ = waitResult(t, executor, 1)
}

func TestSubmitRejectsWhenTaskQueueIsFull(t *testing.T) {
	executor := newTestExecutor(t, 1, 1)
	if err := executor.RegisterWorld(1, 4); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	blocking := testTask{meta: taskMeta(1), run: func(context.Context) (any, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil, nil
	}}
	if _, err := executor.Submit(blocking); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	<-started
	if _, err := executor.Submit(blocking); err != nil {
		t.Fatalf("queued Submit() error = %v", err)
	}
	if _, err := executor.Submit(blocking); !errors.Is(err, ErrTaskQueueFull) {
		t.Fatalf("overflow Submit() error = %v, want ErrTaskQueueFull", err)
	}
	close(release)
	_ = waitResult(t, executor, 1)
	_ = waitResult(t, executor, 1)
}

func TestExecutorReturnsConfiguredTimeoutAndDiscardsLateCompletion(t *testing.T) {
	executor := newTestExecutor(t, 1, 2)
	if err := executor.RegisterWorld(1, 2); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	release := make(chan struct{})
	_, err := executor.Submit(testTask{
		meta: taskMeta(1),
		policy: TaskPolicy{
			Timeout:         10 * time.Millisecond,
			CancelOnTimeout: false,
			TimeoutStatus:   ResultOutcomeUnknown,
		},
		run: func(context.Context) (any, error) {
			<-release
			return "late-success", nil
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	result := waitResult(t, executor, 1)
	if result.Status != ResultOutcomeUnknown {
		t.Fatalf("status = %v, want ResultOutcomeUnknown", result.Status)
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	if got := executor.DrainResults(1, 2); len(got) != 0 {
		t.Fatalf("late result was delivered: %#v", got)
	}
}

func TestTimedOutTaskKeepsWorkerSlotUntilExecutionReturns(t *testing.T) {
	executor := newTestExecutor(t, 1, 4)
	if err := executor.RegisterWorld(1, 4); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	release := make(chan struct{})
	_, err := executor.Submit(testTask{
		meta: taskMeta(1),
		policy: TaskPolicy{
			Timeout:         10 * time.Millisecond,
			CancelOnTimeout: true,
		},
		run: func(context.Context) (any, error) {
			<-release // Deliberately ignore cancellation.
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	if result := waitResult(t, executor, 1); result.Status != ResultTimeout {
		t.Fatalf("status = %v, want ResultTimeout", result.Status)
	}

	secondStarted := make(chan struct{})
	_, err = executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			close(secondStarted)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}
	select {
	case <-secondStarted:
		t.Fatal("second task started while timed-out task still occupied the worker")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second task did not start after the first execution returned")
	}
	_ = waitResult(t, executor, 1)
}

func TestCancelReturnsCanceledResult(t *testing.T) {
	executor := newTestExecutor(t, 1, 2)
	if err := executor.RegisterWorld(1, 2); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	started := make(chan struct{})
	taskID, err := executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-started
	if !executor.Cancel(taskID) {
		t.Fatal("Cancel() = false, want true")
	}
	result := waitResult(t, executor, 1)
	if result.Status != ResultCanceled {
		t.Fatalf("status = %v, want ResultCanceled", result.Status)
	}
}

func TestCanceledTaskKeepsWorkerSlotUntilExecutionReturns(t *testing.T) {
	executor := newTestExecutor(t, 1, 4)
	if err := executor.RegisterWorld(1, 4); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	taskID, err := executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			close(started)
			<-release // Deliberately ignore cancellation.
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	<-started
	if !executor.Cancel(taskID) {
		t.Fatal("Cancel() = false, want true")
	}
	if result := waitResult(t, executor, 1); result.Status != ResultCanceled {
		t.Fatalf("status = %v, want ResultCanceled", result.Status)
	}

	secondStarted := make(chan struct{})
	_, err = executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			close(secondStarted)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}
	select {
	case <-secondStarted:
		t.Fatal("second task started while canceled task still occupied the worker")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second task did not start after the canceled execution returned")
	}
	_ = waitResult(t, executor, 1)
}

func TestTaskPanicBecomesResult(t *testing.T) {
	executor := newTestExecutor(t, 1, 1)
	if err := executor.RegisterWorld(1, 1); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	_, err := executor.Submit(testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			panic("boom")
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	result := waitResult(t, executor, 1)
	if result.Status != ResultPanic || result.Err == nil {
		t.Fatalf("panic result = %#v", result)
	}
}

func TestCloseReturnsCanceledResultForEveryAcceptedTask(t *testing.T) {
	executor, err := New(Config{WorkerCount: 1, TaskQueueSize: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := executor.RegisterWorld(1, 2); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	release := make(chan struct{})
	blocking := testTask{
		meta: taskMeta(1),
		run: func(context.Context) (any, error) {
			<-release
			return nil, nil
		},
	}
	firstID, err := executor.Submit(blocking)
	if err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	secondID, err := executor.Submit(blocking)
	if err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := executor.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	close(release)

	results := executor.DrainResults(1, 2)
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2: %#v", len(results), results)
	}
	seen := map[TaskID]bool{}
	for _, result := range results {
		if result.Status != ResultCanceled {
			t.Fatalf("result status = %v, want ResultCanceled", result.Status)
		}
		seen[result.Meta.TaskID] = true
	}
	if !seen[firstID] || !seen[secondID] {
		t.Fatalf("result task IDs = %#v, want %d and %d", seen, firstID, secondID)
	}
}

func TestUnregisterWorldRejectsLaterSubmissions(t *testing.T) {
	executor := newTestExecutor(t, 1, 1)
	if err := executor.RegisterWorld(1, 1); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	executor.UnregisterWorld(1)
	_, err := executor.Submit(testTask{
		meta: taskMeta(1),
		run:  func(context.Context) (any, error) { return nil, nil },
	})
	if !errors.Is(err, ErrWorldNotFound) {
		t.Fatalf("Submit() error = %v, want ErrWorldNotFound", err)
	}
}
