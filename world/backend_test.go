package world

import (
	"context"
	"testing"
	"time"

	"mmo/backend"
)

type fakeBackend struct {
	registered backend.WorldID
	queueSize  int
	results    []backend.Result
	submitted  backend.Task
	nextTaskID backend.TaskID
}

func (f *fakeBackend) RegisterWorld(worldID backend.WorldID, resultQueueSize int) error {
	f.registered = worldID
	f.queueSize = resultQueueSize
	return nil
}

func (f *fakeBackend) UnregisterWorld(backend.WorldID) {}
func (f *fakeBackend) Submit(task backend.Task) (backend.TaskID, error) {
	f.submitted = task
	f.nextTaskID++
	return f.nextTaskID, nil
}
func (f *fakeBackend) Cancel(backend.TaskID) bool                 { return false }

func (f *fakeBackend) DrainResults(_ backend.WorldID, max int) []backend.Result {
	if max <= 0 || len(f.results) == 0 {
		return nil
	}
	if max > len(f.results) {
		max = len(f.results)
	}
	results := append([]backend.Result(nil), f.results[:max]...)
	f.results = f.results[max:]
	return results
}

func TestWorldIdentityAndEntityEpochAppearInSnapshot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 42
	cfg.RoomID = "dungeon-7"
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entity := mustPlayer(t, w, "p1", Vec2{})
	epoch, ok := w.EntityEpoch(entity)
	if !ok || epoch == 0 {
		t.Fatalf("EntityEpoch(%d) = %d, %v", entity, epoch, ok)
	}
	if w.ID() != 42 || w.RoomID() != "dungeon-7" {
		t.Fatalf("world identity = %d/%q", w.ID(), w.RoomID())
	}

	value, err := w.Snapshot(context.Background(), 0)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	snapshot := value.(Snapshot)
	if snapshot.WorldID != 42 || snapshot.RoomID != "dungeon-7" {
		t.Fatalf("snapshot identity = %d/%q", snapshot.WorldID, snapshot.RoomID)
	}
	if len(snapshot.Entities) != 1 || snapshot.Entities[0].Epoch != epoch {
		t.Fatalf("snapshot entities = %#v, want epoch %d", snapshot.Entities, epoch)
	}
}

func TestNewWorldAutomaticallyAssignsDistinctWorldIDs(t *testing.T) {
	first := mustWorld(t)
	second := mustWorld(t)
	if first.ID() == 0 || second.ID() == 0 || first.ID() == second.ID() {
		t.Fatalf("automatic WorldIDs = %d and %d", first.ID(), second.ID())
	}
}

type worldTestTask struct{ meta backend.TaskMeta }

func (t worldTestTask) Meta() backend.TaskMeta                  { return t.meta }
func (worldTestTask) Policy() backend.TaskPolicy                { return backend.TaskPolicy{} }
func (worldTestTask) Execute(context.Context) (any, error) { return nil, nil }

func TestWorldSubmitsValidatedTaskToAttachedBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 55
	cfg.RoomID = "room-55"
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entity := mustPlayer(t, w, "p1", Vec2{})
	epoch, _ := w.EntityEpoch(entity)
	fake := &fakeBackend{}
	if err := w.AttachBackend(fake); err != nil {
		t.Fatalf("AttachBackend() error = %v", err)
	}
	task := worldTestTask{meta: backend.TaskMeta{
		Kind:        1,
		WorldID:     55,
		RoomID:      "room-55",
		EntityID:    backend.EntityID(entity),
		EntityEpoch: epoch,
	}}
	taskID, err := w.SubmitBackendTask(task)
	if err != nil {
		t.Fatalf("SubmitBackendTask() error = %v", err)
	}
	if taskID != 1 || fake.submitted == nil {
		t.Fatalf("submission = %d, %#v", taskID, fake.submitted)
	}
}

func TestBackendResultIsAppliedOnlyInsideWorldStep(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 7
	cfg.RoomID = "room-7"
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entity := mustPlayer(t, w, "p1", Vec2{})
	epoch, _ := w.EntityEpoch(entity)
	fake := &fakeBackend{}
	if err := w.AttachBackend(fake); err != nil {
		t.Fatalf("AttachBackend() error = %v", err)
	}
	applied := 0
	w.RegisterBackendResultHandler(backend.TaskKind(1), func(_ *World, result backend.Result) {
		if result.Status == backend.ResultSuccess {
			applied++
		}
	})
	taskID, err := w.SubmitBackendTask(worldTestTask{meta: backend.TaskMeta{
		Kind:        1,
		WorldID:     7,
		RoomID:      "room-7",
		EntityID:    backend.EntityID(entity),
		EntityEpoch: epoch,
	}})
	if err != nil {
		t.Fatalf("SubmitBackendTask() error = %v", err)
	}
	fake.results = append(fake.results, backend.Result{Meta: backend.TaskMeta{
		TaskID:      taskID,
		Kind:        1,
		WorldID:     7,
		RoomID:      "room-7",
		EntityID:    backend.EntityID(entity),
		EntityEpoch: epoch,
	}, Status: backend.ResultSuccess})

	if applied != 0 {
		t.Fatal("result was applied outside Step")
	}
	if err := w.Step(context.Background(), 1, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
}

func TestBackendResultWithStaleEntityEpochIsDiscarded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 8
	cfg.RoomID = "room-8"
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entity := mustPlayer(t, w, "p1", Vec2{})
	epoch, _ := w.EntityEpoch(entity)
	fake := &fakeBackend{}
	if err := w.AttachBackend(fake); err != nil {
		t.Fatalf("AttachBackend() error = %v", err)
	}
	applied := 0
	w.RegisterBackendResultHandler(1, func(*World, backend.Result) { applied++ })
	taskID, err := w.SubmitBackendTask(worldTestTask{meta: backend.TaskMeta{
		Kind:        1,
		WorldID:     8,
		RoomID:      "room-8",
		EntityID:    backend.EntityID(entity),
		EntityEpoch: epoch,
	}})
	if err != nil {
		t.Fatalf("SubmitBackendTask() error = %v", err)
	}
	fake.results = []backend.Result{{Meta: backend.TaskMeta{
		TaskID:      taskID,
		Kind:        1,
		WorldID:     8,
		RoomID:      "room-8",
		EntityID:    backend.EntityID(entity),
		EntityEpoch: epoch + 1,
	}}}

	if err := w.Step(context.Background(), 1, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("stale result applied %d times", applied)
	}
}

func TestBackendResultsAreLimitedPerTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 9
	cfg.RoomID = "room-9"
	cfg.MaxBackendResultsPerTick = 1
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	fake := &fakeBackend{}
	if err := w.AttachBackend(fake); err != nil {
		t.Fatalf("AttachBackend() error = %v", err)
	}
	applied := 0
	w.RegisterBackendResultHandler(1, func(*World, backend.Result) { applied++ })
	firstID, err := w.SubmitBackendTask(worldTestTask{meta: backend.TaskMeta{
		Kind: 1, WorldID: 9, RoomID: "room-9",
	}})
	if err != nil {
		t.Fatalf("first SubmitBackendTask() error = %v", err)
	}
	secondID, err := w.SubmitBackendTask(worldTestTask{meta: backend.TaskMeta{
		Kind: 1, WorldID: 9, RoomID: "room-9",
	}})
	if err != nil {
		t.Fatalf("second SubmitBackendTask() error = %v", err)
	}
	fake.results = []backend.Result{
		{Meta: backend.TaskMeta{TaskID: firstID, Kind: 1, WorldID: 9, RoomID: "room-9"}},
		{Meta: backend.TaskMeta{TaskID: secondID, Kind: 1, WorldID: 9, RoomID: "room-9"}},
	}

	if err := w.Step(context.Background(), 1, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("first tick applied = %d, want 1", applied)
	}
	if err := w.Step(context.Background(), 2, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if applied != 2 {
		t.Fatalf("second tick applied total = %d, want 2", applied)
	}
}

func TestBackendResultIsAppliedAtMostOncePerTaskID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorldID = 10
	cfg.RoomID = "room-10"
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	fake := &fakeBackend{}
	if err := w.AttachBackend(fake); err != nil {
		t.Fatalf("AttachBackend() error = %v", err)
	}
	applied := 0
	w.RegisterBackendResultHandler(1, func(*World, backend.Result) { applied++ })
	taskID, err := w.SubmitBackendTask(worldTestTask{meta: backend.TaskMeta{
		Kind: 1, WorldID: 10, RoomID: "room-10",
	}})
	if err != nil {
		t.Fatalf("SubmitBackendTask() error = %v", err)
	}
	result := backend.Result{Meta: backend.TaskMeta{
		TaskID: taskID, Kind: 1, WorldID: 10, RoomID: "room-10",
	}}
	fake.results = []backend.Result{result, result}

	if err := w.Step(context.Background(), 1, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("duplicate result applied %d times, want 1", applied)
	}
}
