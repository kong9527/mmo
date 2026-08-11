package update

import (
	"context"
	"time"
)

// combatSystem 负责战斗结算：冷却计时和攻击伤害处理。
//
// 每个 Tick 执行两步：
//  1. 遍历所有实体的 Combat 组件，扣减攻击冷却
//  2. 遍历由 AI/玩家在本帧生成的 attackIntents，逐一结算攻击
//
// 在 aiSystem 之后执行，消耗 AI 生成的攻击意图。
type combatSystem struct{}

// Name 返回子系统名称。
func (combatSystem) Name() string { return "combat" }

// Update 推进冷却计时并结算本帧所有攻击意图。
func (combatSystem) Update(ctx context.Context, world *World, tick TickContext) error {
	// 第一步：所有实体的冷却计时器按 Tick Delta 衰减。
	for i := range world.combat.values {
		combat := &world.combat.values[i]
		combat.Remaining -= tick.Delta
		if combat.Remaining < 0 {
			combat.Remaining = 0
		}
	}

	// 第二步：结算本帧收集的攻击意图。
	for i, intent := range world.attackIntents {
		if i&127 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		world.resolveAttack(intent)
	}
	// 清空，下帧复用
	world.attackIntents = world.attackIntents[:0]
	return nil
}

// resolveAttack 结算一次攻击意图。
//
// 前置检查（任一不满足则跳过）：
//  1. 攻击者和目标都存活
//  2. 双方的组件数据存在
//  3. 攻击者冷却已就绪（Remaining <= 0）
//  4. 目标在攻击范围内
//
// 伤害 = Combat.Damage + 攻击力 Buff（BuffAttackPower）的 Magnitude 总和。
// 结算后将冷却计时器重置为 Cooldown。
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

	// 基础伤害 + 攻击力 Buff 加成
	damage := combat.Damage + w.buffMagnitude(intent.attacker, BuffAttackPower)
	if damage < 0 {
		damage = 0
	}
	// 重置冷却
	combat.Remaining = combat.Cooldown
	w.emit(Event{
		Type:   EventAttack,
		Entity: intent.attacker,
		Target: intent.target,
		Amount: damage,
	})
	w.applyDamage(intent.attacker, intent.target, damage)
}

// applyDamage 对目标实体扣减生命值。
// 若 HP 归零则设置 Dead = true（死亡由 cleanupSystem 在后续处理）。
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

// subtractDuration 安全地扣减 Duration，下界为 0。
func subtractDuration(value, delta time.Duration) time.Duration {
	value -= delta
	if value < 0 {
		return 0
	}
	return value
}
