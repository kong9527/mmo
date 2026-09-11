package example

import (
	"context"
	"errors"
	"time"

	"mmo/backend"
	"mmo/world"
)

const TaskKindLoadPlayerHealth backend.TaskKind = 1

type PlayerHealthResult struct {
	Health world.Health
}

// LoadPlayerHealthTask simulates loading persistent player state.
// It carries immutable IDs and value snapshots, never a World or Entity pointer.
type LoadPlayerHealthTask struct {
	meta   backend.TaskMeta
	health world.Health
	delay  time.Duration
}

func NewLoadPlayerHealthTask(
	w *world.World,
	entity world.EntityID,
	health world.Health,
	delay time.Duration,
) (LoadPlayerHealthTask, error) {
	if w == nil {
		return LoadPlayerHealthTask{}, errors.New("world is required")
	}
	epoch, ok := w.EntityEpoch(entity)
	if !ok {
		return LoadPlayerHealthTask{}, world.ErrEntityNotFound
	}
	return LoadPlayerHealthTask{
		meta: backend.TaskMeta{
			Kind:        TaskKindLoadPlayerHealth,
			WorldID:     backend.WorldID(w.ID()),
			RoomID:      w.RoomID(),
			EntityID:    backend.EntityID(entity),
			EntityEpoch: epoch,
			SubmitTick:  w.Tick(),
		},
		health: health,
		delay:  delay,
	}, nil
}

func (t LoadPlayerHealthTask) Meta() backend.TaskMeta { return t.meta }

func (t LoadPlayerHealthTask) Policy() backend.TaskPolicy {
	return backend.TaskPolicy{
		Timeout:         time.Second,
		CancelOnTimeout: true,
		TimeoutStatus:   backend.ResultTimeout,
	}
}

func (t LoadPlayerHealthTask) Execute(ctx context.Context) (any, error) {
	timer := time.NewTimer(t.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return PlayerHealthResult{Health: t.health}, nil
	}
}

func RegisterLoadPlayerHealthHandler(w *world.World) {
	w.RegisterBackendResultHandler(
		TaskKindLoadPlayerHealth,
		func(w *world.World, result backend.Result) {
			if result.Status != backend.ResultSuccess {
				return
			}
			payload, ok := result.Data.(PlayerHealthResult)
			if !ok {
				return
			}
			_ = w.SetHealth(world.EntityID(result.Meta.EntityID), payload.Health)
		},
	)
}

