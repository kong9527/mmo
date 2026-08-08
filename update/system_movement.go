package update

import "context"

type movementSystem struct{}

func (movementSystem) Name() string { return "movement" }

func (movementSystem) Update(ctx context.Context, world *World, tick TickContext) error {
	seconds := tick.Delta.Seconds()
	for i, id := range world.movements.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if !world.living(id) {
			continue
		}
		transform, ok := world.transforms.get(id)
		if !ok {
			continue
		}
		movement := &world.movements.values[i]
		maximum := movement.MaxSpeed + world.buffMagnitude(id, BuffMoveSpeed)
		movement.Velocity = clampMagnitude(movement.Velocity, maximum)
		if movement.Velocity == (Vec2{}) {
			continue
		}
		transform.Position = transform.Position.Add(movement.Velocity.Scale(seconds))
		transform.Facing = movement.Velocity.Normalize()
		world.emit(Event{
			Type:     EventMoved,
			Entity:   id,
			Position: transform.Position,
		})
	}
	return nil
}
