package world

import "context"

// aiSystem 负责怪物的自主行为：选择目标和追击移动。
//
// 每个有 AI 组件的实体（通常为怪物）在每 Tick 中：
//  1. 若当前目标不在仇恨范围内，扫描附近玩家重新选目标
//  2. 若目标在攻击范围内，停止移动并生成攻击意图
//  3. 否则向目标方向移动
//
// 在 movementSystem 之后执行，使得 AI 设定的速度在下一帧被移动系统消费；
// 在 combatSystem 之前执行，使得攻击意图在本帧即可结算。
type aiSystem struct{}

// Name 返回子系统名称。
func (aiSystem) Name() string { return "ai" }

// Update 遍历所有 AI 实体，逐帧更新目标选择和移动意图。
func (aiSystem) Update(ctx context.Context, world *World, _ TickContext) error {
	for i, id := range world.ai.entities {
		// 每 128 个实体检查一次 ctx 是否已取消，
		// 在响应性与性能之间取得平衡。
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

// validAITarget 判断当前追踪目标是否仍然有效。
// 条件：目标存在、存活、是玩家、且在 aggroRange 范围内。
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

// nearestLivingPlayer 在 maximumDistance 内查找最近且存活的玩家。
// 距离相等时 ID 较小者优先，保证确定性。
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
