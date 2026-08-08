package update

import (
	"context"
	"sort"
)

func (w *World) Snapshot(ctx context.Context, tick uint64) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ids := make([]EntityID, 0, len(w.entities))
	for id, record := range w.entities {
		if record.active {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	entities := make([]EntitySnapshot, 0, len(ids))
	for i, id := range ids {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		record := w.entities[id]
		entity := EntitySnapshot{ID: id, Kind: record.kind}
		if transform, ok := w.transforms.get(id); ok {
			entity.Position = transform.Position
			entity.Facing = transform.Facing
		}
		if movement, ok := w.movements.get(id); ok {
			entity.Velocity = movement.Velocity
		}
		if health, ok := w.health.get(id); ok {
			entity.Health = health.Current
			entity.MaxHealth = health.Maximum
			entity.Dead = health.Dead
		}
		if ai, ok := w.ai.get(id); ok {
			entity.Target = ai.Target
		}
		if state, ok := w.playerState.get(id); ok {
			entity.PlayerID = state.PlayerID
		}
		if buffs, ok := w.buffs.get(id); ok {
			entity.Buffs = append([]Buff(nil), (*buffs)...)
		}
		entities = append(entities, entity)
	}

	return Snapshot{
		Tick:     tick,
		Entities: entities,
		Events:   append([]Event(nil), w.events...),
	}, nil
}
