package update

import "context"

// cleanupSystem 负责清理死亡或已标记非活跃的实体。
//
// 遍历所有 Health 组件，对满足以下任一条件的实体产出 EventDeath 并加入删除队列：
//  1. 已标记非活跃（Active == false）
//  2. Dead == true 或 Current <= 0
//
// 作为最后一个子系统执行，确保其他子系统在本 Tick 内已产生死亡事件后
// 再统一加入删除队列，由 Step 末尾的 flushStructuralChanges 实际移除。
type cleanupSystem struct{}

// Name 返回子系统名称。
func (cleanupSystem) Name() string { return "cleanup" }

// Update 遍历实体查找死亡/非活跃者，产出死亡事件并加入删除队列。
func (cleanupSystem) Update(ctx context.Context, world *World, _ TickContext) error {
	for i, id := range world.health.entities {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		health := &world.health.values[i]
		// 存活且健康的实体跳过。
		if !world.Active(id) || (!health.Dead && health.Current > 0) {
			continue
		}
		// 归零并标记死亡，确保后续查询一致性。
		health.Current = 0
		health.Dead = true
		world.emit(Event{Type: EventDeath, Entity: id})
		// 加入延迟删除队列，实际移除在 flushStructuralChanges 中执行。
		if err := world.queueRemoval(id); err != nil {
			return err
		}
	}
	return nil
}
