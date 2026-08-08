package update

import "context"

type aiSystem struct{}

func (aiSystem) Name() string { return "ai" }

func (aiSystem) Update(ctx context.Context, world *World, _ TickContext) error {
	for i, id := range world.ai.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if !world.living(id) {
			continue
		}
		ai := &world.ai.values[i]
		origin, ok := world.transforms.get(id)
		if !ok {
			continue
		}

		target := ai.Target
		if !world.validAITarget(id, target, ai.AggroRange) {
			target = world.nearestLivingPlayer(origin.Position, ai.AggroRange)
			ai.Target = target
		}
		movement, ok := world.movements.get(id)
		if !ok {
			continue
		}
		if target == 0 {
			movement.Velocity = Vec2{}
			continue
		}

		targetTransform, _ := world.transforms.get(target)
		offset := targetTransform.Position.Sub(origin.Position)
		combat, hasCombat := world.combat.get(id)
		if hasCombat && offset.Length() <= combat.Range {
			movement.Velocity = Vec2{}
			world.attackIntents = append(world.attackIntents, attackIntent{
				attacker: id,
				target:   target,
			})
			continue
		}
		movement.Velocity = offset.Normalize().Scale(ai.MoveSpeed)
	}
	return nil
}

func (w *World) validAITarget(observer, target EntityID, aggroRange float64) bool {
	if target == 0 || !w.living(target) {
		return false
	}
	record, ok := w.entities[target]
	if !ok || record.kind != EntityPlayer {
		return false
	}
	observerTransform, observerOK := w.transforms.get(observer)
	targetTransform, targetOK := w.transforms.get(target)
	if !observerOK || !targetOK {
		return false
	}
	return distance(observerTransform.Position, targetTransform.Position) <= aggroRange
}

func (w *World) nearestLivingPlayer(origin Vec2, maximumDistance float64) EntityID {
	var selected EntityID
	selectedDistance := maximumDistance
	for _, id := range w.playerState.entities {
		if !w.living(id) {
			continue
		}
		transform, ok := w.transforms.get(id)
		if !ok {
			continue
		}
		currentDistance := distance(origin, transform.Position)
		if currentDistance > maximumDistance {
			continue
		}
		if selected == 0 || currentDistance < selectedDistance ||
			(currentDistance == selectedDistance && id < selected) {
			selected = id
			selectedDistance = currentDistance
		}
	}
	return selected
}
