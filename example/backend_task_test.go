package example

import (
	"context"
	"testing"
	"time"

	"mmo/backend"
	"mmo/gameloop"
	"mmo/world"
)

func TestLoadPlayerHealthTaskReturnsThroughLaterWorldTick(t *testing.T) {
	worldConfig := world.DefaultConfig()
	worldConfig.WorldID = 100
	worldConfig.RoomID = "starter-zone"
	_, w, executor, err := NewMMOLoopWithBackend(
		gameloop.DefaultConfig(),
		worldConfig,
		backend.Config{WorkerCount: 1, TaskQueueSize: 8},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewMMOLoopWithBackend() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := executor.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	entity, err := w.AddPlayer(world.PlayerSpec{
		PlayerID: "player-1",
		Health:   world.Health{Current: 10, Maximum: 100},
	})
	if err != nil {
		t.Fatalf("AddPlayer() error = %v", err)
	}
	task, err := NewLoadPlayerHealthTask(
		w,
		entity,
		world.Health{Current: 80, Maximum: 100},
		40*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewLoadPlayerHealthTask() error = %v", err)
	}
	if _, err := w.SubmitBackendTask(task); err != nil {
		t.Fatalf("SubmitBackendTask() error = %v", err)
	}

	if err := w.Step(context.Background(), 1, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	health, _ := w.Health(entity)
	if health.Current != 10 {
		t.Fatalf("health changed before task completion: %v", health.Current)
	}

	time.Sleep(60 * time.Millisecond)
	if err := w.Step(context.Background(), 2, 50*time.Millisecond, nil); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	health, _ = w.Health(entity)
	if health.Current != 80 {
		t.Fatalf("health after Backend result = %v, want 80", health.Current)
	}
}
