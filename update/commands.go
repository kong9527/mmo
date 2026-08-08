package update

import (
	"fmt"

	"mmo/gameloop"
)

func (w *World) handleCommand(command gameloop.Command) error {
	switch payload := command.Payload.(type) {
	case MoveCommand:
		return w.withPlayerCommand(command, func(player EntityID) error {
			movement, ok := w.movements.get(player)
			if !ok {
				return fmt.Errorf("%w: movement component for %d", ErrEntityNotFound, player)
			}
			movement.Velocity = payload.Velocity
			return nil
		})

	case AttackCommand:
		return w.withPlayerCommand(command, func(player EntityID) error {
			if !w.Exists(payload.Target) {
				return fmt.Errorf("%w: attack target %d", ErrEntityNotFound, payload.Target)
			}
			w.attackIntents = append(w.attackIntents, attackIntent{
				attacker: player,
				target:   payload.Target,
			})
			return nil
		})

	case ApplyBuffCommand:
		if command.PlayerID == "" {
			return w.applyBuffNow(0, payload.Target, payload.Buff)
		}
		return w.withPlayerCommand(command, func(player EntityID) error {
			return w.applyBuffNow(player, payload.Target, payload.Buff)
		})

	case SpawnMonsterCommand:
		spec := payload.Spec
		w.pendingSpawns = append(w.pendingSpawns, spawnRequest{monster: &spec})
		return nil

	case DespawnCommand:
		return w.queueRemoval(payload.Entity)

	default:
		return fmt.Errorf("%w: payload %T", ErrUnsupportedCommand, command.Payload)
	}
}

func (w *World) withPlayerCommand(command gameloop.Command, apply func(EntityID) error) error {
	player, ok := w.players[command.PlayerID]
	if !ok || !w.Active(player) {
		return fmt.Errorf("%w: %s", ErrPlayerNotFound, command.PlayerID)
	}
	state, ok := w.playerState.get(player)
	if !ok {
		return fmt.Errorf("%w: player state for %d", ErrEntityNotFound, player)
	}
	if command.Seq > 0 && command.Seq <= state.LastSeq {
		return nil
	}
	if err := apply(player); err != nil {
		return err
	}
	if command.Seq > 0 {
		state.LastSeq = command.Seq
	}
	return nil
}

func (w *World) applyBuffNow(source, target EntityID, buff Buff) error {
	if !w.living(target) {
		return fmt.Errorf("%w: buff target %d", ErrEntityNotFound, target)
	}
	if buff.Kind == BuffUnknown {
		return fmt.Errorf("%w: buff kind is required", ErrInvalidCommand)
	}
	if buff.Duration <= 0 {
		return fmt.Errorf("%w: buff duration must be positive", ErrInvalidCommand)
	}
	if buff.Kind == BuffDamageOverTime && buff.Period <= 0 {
		return fmt.Errorf("%w: damage-over-time period must be positive", ErrInvalidCommand)
	}
	if buff.ID == "" {
		w.nextBuffID++
		buff.ID = fmt.Sprintf("buff-%d", w.nextBuffID)
	}
	buff.Source = source
	buff.Remaining = buff.Duration
	buff.UntilNext = buff.Period

	list, ok := w.buffs.get(target)
	if !ok {
		w.buffs.set(target, []Buff{buff})
	} else {
		replaced := false
		for i := range *list {
			if (*list)[i].ID == buff.ID {
				(*list)[i] = buff
				replaced = true
				break
			}
		}
		if !replaced {
			*list = append(*list, buff)
		}
	}
	w.emit(Event{
		Type:   EventBuffApplied,
		Entity: source,
		Target: target,
		BuffID: buff.ID,
	})
	return nil
}

func (w *World) buffMagnitude(id EntityID, kind BuffKind) float64 {
	list, ok := w.buffs.get(id)
	if !ok {
		return 0
	}
	var total float64
	for _, buff := range *list {
		if buff.Kind == kind && buff.Remaining > 0 {
			total += buff.Magnitude
		}
	}
	return total
}
