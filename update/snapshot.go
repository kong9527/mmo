package update

import (
	"context"
	"sort"
)

// Snapshot 实现 gameloop.World 接口，生成世界在当前 Tick 的只读快照。
//
// 遍历所有活跃实体，从各组件 store 中聚合数据组装 EntitySnapshot。
// 对 ctx 的检查每 128 个实体执行一次，平衡精度与开销，支持超时/取消。
//
// 参考：游戏循环模式——定期将只读快照发布给网络层或持久化层。
func (w *World) Snapshot(ctx context.Context, tick uint64) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ids := make([]EntityID, 0, len(w.entities))
	for id, record := range w.entities {
		if record.active {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	entities := make([]EntitySnapshot, 0, len(ids))
	for i, id := range ids {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		record := w.entities[id]
		entity := EntitySnapshot{ID: id, Kind: record.kind}
		if transform, ok := w.transforms.get(id); ok {
			entity.Position = transform.Position
			entity.Facing = transform.Facing
		}
		if movement, ok := w.movements.get(id); ok {
			entity.Velocity = movement.Velocity
		}
		if health, ok := w.health.get(id); ok {
			entity.Health = health.Current
			entity.MaxHealth = health.Maximum
			entity.Dead = health.Dead
		}
		if ai, ok := w.ai.get(id); ok {
			entity.Target = ai.Target
		}
		if state, ok := w.playerState.get(id); ok {
			entity.PlayerID = state.PlayerID
		}
		if buffs, ok := w.buffs.get(id); ok {
			entity.Buffs = append([]Buff(nil), *buffs...)
		}
		entities = append(entities, entity)
	}

	return Snapshot{
		Tick:     tick,
		Entities: entities,
		Events:   append([]Event(nil), w.events...),
	}, nil
}
