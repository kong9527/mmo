package update

import "context"

type recoverySystem struct{}

func (recoverySystem) Name() string { return "recovery" }

func (recoverySystem) Update(ctx context.Context, world *World, tick TickContext) error {
	for i, id := range world.health.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		health := &world.health.values[i]
		if !world.Active(id) || health.Dead || health.RecoveryPerSecond <= 0 ||
			health.Current >= health.Maximum {
			continue
		}
		before := health.Current
		health.Current += health.RecoveryPerSecond * tick.Delta.Seconds()
		if health.Current > health.Maximum {
			health.Current = health.Maximum
		}
		world.emit(Event{
			Type:   EventRecovered,
			Entity: id,
			Amount: health.Current - before,
		})
	}
	return nil
}
