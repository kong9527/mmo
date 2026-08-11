package update

import "context"

// recoverySystem 负责实体的自然生命回复。
//
// 每个 Tick 遍历所有有 Health 组件的实体，对满足以下条件的进行回复：
//  — 活跃且未死亡
//  — RecoveryPerSecond > 0
//  — 当前生命值未满
//
// 回复量 = RecoveryPerSecond × dt，钳制不超过 Maximum。
//
// 在 buffSystem 之后执行，使得 DoT 扣血先于自然回复，
// 避免刚扣血在同一 Tick 内被回复抵消。
type recoverySystem struct{}

// Name 返回子系统名称。
func (recoverySystem) Name() string { return "recovery" }

// Update 遍历实体执行自然生命回复并产出 EventRecovered 事件。
func (recoverySystem) Update(ctx context.Context, world *World, tick TickContext) error {
	for i, id := range world.health.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		health := &world.health.values[i]
		// 跳过不满足回复条件的实体。
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
