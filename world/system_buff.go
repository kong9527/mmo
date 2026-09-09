package world

import "context"

// buffSystem 负责 Buff 的生命周期管理：计时、周期触发和过期移除。
//
// 每个 Tick 遍历所有实体的 Buff 列表：
//  1. 周期型 Buff（BuffDamageOverTime）：按 Period 触发效果
//  2. 扣减剩余时间，过期则产出 EventBuffExpired 并从列表移除
//
// 在 combatSystem 之后执行，使得本 Tick 施加的 Buff 立即生效；
// 在 recoverySystem 之前执行，使得自然回复在 Buff 扣血后计算。
type buffSystem struct{}

// Name 返回子系统名称。
func (buffSystem) Name() string { return "buff" }

// Update 遍历所有实体的 Buff 列表，执行周期触发和过期清理。
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
			// 在本 Tick 中 Buff 实际有效的时间（不超过剩余时间）。
			activeDelta := tick.Delta
			if activeDelta > buff.Remaining {
				activeDelta = buff.Remaining
			}

			// 持续伤害 (DoT)：在有效时间内按 Period 循环触发。
			if buff.Kind == BuffDamageOverTime && buff.Period > 0 && activeDelta > 0 {
				buff.UntilNext -= activeDelta
				for buff.UntilNext <= 0 {
					world.applyDamage(buff.Source, id, buff.Magnitude)
					buff.UntilNext += buff.Period
					// 如果伤害导致死亡，提前终止后续触发。
					if !world.living(id) {
						break
					}
				}
			}

			// 扣减 Buff 剩余时间，过期则产出事件并丢弃。
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
		// 清理切片尾部可能残留的旧 Buff 值，避免 GC 长期引用。
		for clearIndex := len(kept); clearIndex < len(buffs); clearIndex++ {
			buffs[clearIndex] = Buff{}
		}
		world.buffs.values[i] = kept
	}
	return nil
}
