package update

import (
	"context"
	"time"
)

type combatSystem struct{}

func (combatSystem) Name() string { return "combat" }

func (combatSystem) Update(ctx context.Context, world *World, tick TickContext) error {
	for i := range world.combat.values {
		combat := &world.combat.values[i]
		combat.Remaining -= tick.Delta
		if combat.Remaining < 0 {
			combat.Remaining = 0
		}
	}

	for i, intent := range world.attackIntents {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		world.resolveAttack(intent)
	}
	world.attackIntents = world.attackIntents[:0]
	return nil
}

func (w *World) resolveAttack(intent attackIntent) {
	if !w.living(intent.attacker) || !w.living(intent.target) {
		return
	}
	combat, combatOK := w.combat.get(intent.attacker)
	attackerTransform, attackerOK := w.transforms.get(intent.attacker)
	targetTransform, targetOK := w.transforms.get(intent.target)
	if !combatOK || !attackerOK || !targetOK || combat.Remaining > 0 {
		return
	}
	if distance(attackerTransform.Position, targetTransform.Position) > combat.Range {
		return
	}

	damage := combat.Damage + w.buffMagnitude(intent.attacker, BuffAttackPower)
	if damage < 0 {
		damage = 0
	}
	combat.Remaining = combat.Cooldown
	w.emit(Event{
		Type:   EventAttack,
		Entity: intent.attacker,
		Target: intent.target,
		Amount: damage,
	})
	w.applyDamage(intent.attacker, intent.target, damage)
}

func (w *World) applyDamage(source, target EntityID, amount float64) {
	if amount <= 0 || !w.living(target) {
		return
	}
	health, ok := w.health.get(target)
	if !ok {
		return
	}
	health.Current -= amount
	if health.Current <= 0 {
		health.Current = 0
		health.Dead = true
	}
	w.emit(Event{
		Type:   EventDamage,
		Entity: source,
		Target: target,
		Amount: amount,
	})
}

func subtractDuration(value, delta time.Duration) time.Duration {
	value -= delta
	if value < 0 {
		return 0
	}
	return value
}
