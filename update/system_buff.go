package update

import "context"

type buffSystem struct{}

func (buffSystem) Name() string { return "buff" }

func (buffSystem) Update(ctx context.Context, world *World, tick TickContext) error {
	for i, id := range world.buffs.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if !world.Active(id) {
			continue
		}
		buffs := world.buffs.values[i]
		kept := buffs[:0]
		for _, buff := range buffs {
			activeDelta := tick.Delta
			if activeDelta > buff.Remaining {
				activeDelta = buff.Remaining
			}

			if buff.Kind == BuffDamageOverTime && buff.Period > 0 && activeDelta > 0 {
				buff.UntilNext -= activeDelta
				for buff.UntilNext <= 0 {
					world.applyDamage(buff.Source, id, buff.Magnitude)
					buff.UntilNext += buff.Period
					if !world.living(id) {
						break
					}
				}
			}

			buff.Remaining = subtractDuration(buff.Remaining, tick.Delta)
			if buff.Remaining == 0 {
				world.emit(Event{
					Type:   EventBuffExpired,
					Entity: id,
					BuffID: buff.ID,
				})
				continue
			}
			kept = append(kept, buff)
		}
		for clearIndex := len(kept); clearIndex < len(buffs); clearIndex++ {
			buffs[clearIndex] = Buff{}
		}
		world.buffs.values[i] = kept
	}
	return nil
}
