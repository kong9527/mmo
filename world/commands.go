package world

import (
	"fmt"

	"mmo/gameloop"
)

// handleCommand 根据 Payload 类型分发命令到对应的处理器。
//
// 支持的命令类型：
//   - MoveCommand      → 更新玩家速度
//   - AttackCommand    → 记录攻击意图（在 combatSystem 中结算）
//   - ApplyBuffCommand → 立即或由玩家施加 Buff
//   - SpawnMonsterCommand → 加入延迟创建队列
//   - DespawnCommand   → 加入延迟删除队列
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

// withPlayerCommand 将 command.PlayerID 解析为实体 ID，并执行 apply 回调。
//
// 提供两个保护：
//  1. 玩家不存在或已离线时返回 ErrPlayerNotFound
//  2. 基于 Seq 的去重/乱序保护：Seq ≤ LastSeq 的命令直接丢弃
//
// apply 成功后更新 LastSeq。
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

// applyBuffNow 直接对目标实体施加 Buff。
//
// 校验规则：
//   - 目标必须存活
//   - Kind 不能为 BuffUnknown
//   - Duration 必须 > 0
//   - 持续伤害 (BuffDamageOverTime) 的 Period 必须 > 0
//
// Buff.ID 为空时自动生成 "buff-N" 格式的唯一 ID。
// 同名 Buff 会原地替换，不同名 Buff 追加到列表。
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

// buffMagnitude 查询某类型 Buff 在实体上的总效果量级。
// 返回所有同类活跃 Buff 的 Magnitude 之和。
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
