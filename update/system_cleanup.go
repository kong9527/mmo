package update

import "context"

type cleanupSystem struct{}

func (cleanupSystem) Name() string { return "cleanup" }

func (cleanupSystem) Update(ctx context.Context, world *World, _ TickContext) error {
	for i, id := range world.health.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		health := &world.health.values[i]
		if !world.Active(id) || (!health.Dead && health.Current > 0) {
			continue
		}
		health.Current = 0
		health.Dead = true
		world.emit(Event{Type: EventDeath, Entity: id})
		if err := world.queueRemoval(id); err != nil {
			return err
		}
	}
	return nil
}
