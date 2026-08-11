package update

import "context"

// movementSystem 负责将实体的速度向量积分到世界坐标。
//
// 每个 Tick 对所有有 Movement 组件的存活实体：
//  1. 应用移速 Buff（BuffMoveSpeed）修正最大速度
//  2. 钳制速度不超过上限
//  3. 位置 = 位置 + 速度 × dt
//  4. 朝向 = 速度方向的单位向量
//
// 作为第一个子系统执行，将命令和 AI 在前一帧设定的速度落位到坐标上，
// 后续子系统（AI、战斗等）基于新位置进行判断。
type movementSystem struct{}

// Name 返回子系统名称。
func (movementSystem) Name() string { return "movement" }

// Update 遍历所有实体，将速度积分到位置并产出移动事件。
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
		// 最大速度 = 基础 MaxSpeed + 移速 Buff 的总 Magnitude。
		maximum := movement.MaxSpeed + world.buffMagnitude(id, BuffMoveSpeed)
		movement.Velocity = clampMagnitude(movement.Velocity, maximum)
		// 静止实体跳过，避免产出无意义的移动事件。
		if movement.Velocity == (Vec2{}) {
			continue
		}
		// 位置积分：P' = P + V × dt
		transform.Position = transform.Position.Add(movement.Velocity.Scale(seconds))
		// 朝向跟随速度方向
		transform.Facing = movement.Velocity.Normalize()
		world.emit(Event{
			Type:     EventMoved,
			Entity:   id,
			Position: transform.Position,
		})
	}
	return nil
}
