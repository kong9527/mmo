package world

import (
	"context"
	"fmt"
	"time"

	"mmo/gameloop"
)

// 编译期断言：*World 实现了 gameloop.World 接口。
var _ gameloop.World = (*World)(nil)

// entityRecord 记录实体的分类和活跃状态。
// 轻量元数据，组件数据分别存储在各自的 store 中。
type entityRecord struct {
	kind   EntityKind // 实体类别
	active bool       // 是否活跃（false 表示已标记待删除）
}

// spawnRequest 是延迟创建的请求，在 Tick 末尾统一处理。
type spawnRequest struct {
	monster *MonsterSpec // 待生成的怪物；nil 表示无效请求
}

// attackIntent 记录一次攻击意图（攻击者 → 目标），
// 由命令处理阶段收集，在 combatSystem 中统一结算。
type attackIntent struct {
	attacker EntityID // 攻击发起者
	target   EntityID // 攻击目标
}

// TickContext 封装一次逻辑 Tick 的元信息，传递给各 system。
type TickContext struct {
	Tick  uint64        // 当前逻辑 Tick 序号
	Delta time.Duration // 本 Tick 的逻辑时间步长（固定）
}

// system 是内部子系统的接口。
// 每个子系统负责一个领域（移动、AI、战斗、Buff、回复、清理），
// 在 Step 中按注册顺序依次执行。
type system interface {
	// Name 返回子系统名称，用于日志和错误定位。
	Name() string
	// Update 在本 Tick 内遍历所有实体，执行该领域的逻辑。
	Update(context.Context, *World, TickContext) error
}

// World 是 Update 系统的核心——实现 gameloop.World 接口的模拟世界。
//
// 参考：更新方法模式——游戏世界管理对象集合，每帧遍历每个对象并调用 update()。
// 结合组件模式，World 不是直接存储 Entity 对象，而是使用按领域的 store 分别存储
// 各组件的状态数据（SoA / 数据局部性）。
//
// 并发模型：所有状态写操作仅在 Step 内单 goroutine 执行，
// 外部查询（Exists, Transform, Health 等）通过读安全的 store 访问。
type World struct {
	cfg Config

	// 内部 ID 分配
	nextEntityID EntityID // 下一个实体 ID
	nextBuffID   uint64   // 下一个 Buff ID

	// Tick 状态
	tick           uint64   // 已完成的最新 Tick 序号
	processingTick uint64   // 正在执行中的 Tick 序号（0 = 不在 Step 中）
	stepping       bool     // 是否正在 Step 调用中（防止重入）
	lastSpawned    EntityID // 最近创建的实体 ID

	// 实体索引
	entities map[EntityID]entityRecord // 实体 ID → 元数据
	players  map[string]EntityID       // 玩家外部 ID → 实体 ID

	// 组件数据存储（按领域分离，缓存友好）
	transforms  *store[Transform]   // 位置 & 朝向
	movements   *store[Movement]    // 速度状态
	health      *store[Health]      // 生命值
	combat      *store[Combat]      // 战斗属性
	ai          *store[AI]          // AI 状态
	playerState *store[PlayerState] // 玩家网络元数据
	buffs       *store[[]Buff]      // 实体身上的 Buff 列表

	// 子系统执行顺序
	systems []system
	// 本 Tick 产生的事件（发布后清空）
	events []Event

	// World 内部事件队列。
	// 用于系统间解耦通知，不替代 Snapshot 中的事件输出。
	eventQueue *EventQueue

	// 本 Tick 内收集的攻击意图
	attackIntents []attackIntent
	// 延迟执行的创建/删除请求
	pendingSpawns   []spawnRequest
	pendingRemovals []EntityID
}

// NewWorld 创建并初始化一个新的游戏世界。
// 按 cfg.MaxEntities 预分配各组件存储的容量，并注册子系统执行顺序：
//
//	movement → ai → combat → buff → recovery → cleanup
func NewWorld(cfg Config) (*World, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	capacity := cfg.MaxEntities
	w := &World{
		cfg:             cfg,
		entities:        make(map[EntityID]entityRecord, capacity),
		players:         make(map[string]EntityID, capacity),
		transforms:      newStore[Transform](capacity),
		movements:       newStore[Movement](capacity),
		health:          newStore[Health](capacity),
		combat:          newStore[Combat](capacity),
		ai:              newStore[AI](capacity),
		playerState:     newStore[PlayerState](capacity),
		buffs:           newStore[[]Buff](capacity),
		events:          make([]Event, 0, capacity),
		eventQueue:      NewEventQueue(128),
		attackIntents:   make([]attackIntent, 0, capacity),
		pendingSpawns:   make([]spawnRequest, 0, 16),
		pendingRemovals: make([]EntityID, 0, 16),
	}
	w.systems = []system{
		movementSystem{},
		aiSystem{},
		combatSystem{},
		buffSystem{},
		recoverySystem{},
		cleanupSystem{},
	}
	return w, nil
}

// Config 返回世界的运行配置（只读）。
func (w *World) Config() Config {
	return w.cfg
}

// SubscribeEvent 注册 World 内部事件监听器。
// 现有 Snapshot 事件流程保持不变，新系统可逐步迁移到这里。
func (w *World) SubscribeEvent(eventType EventType, handler EventHandler) {
	w.eventQueue.Subscribe(eventType, handler)
}

// Tick 返回已完成的最新逻辑 Tick 序号。
// 如果 Step 正在执行中，返回的是上一帧的序号。
func (w *World) Tick() uint64 {
	return w.tick
}

// LastSpawnedEntity 返回最近创建的实体 ID，常用于测试断言。
func (w *World) LastSpawnedEntity() EntityID {
	return w.lastSpawned
}

// Exists 判断实体 ID 是否存在于世界中（包括已标记非活跃的实体）。
func (w *World) Exists(id EntityID) bool {
	_, ok := w.entities[id]
	return ok
}

// Active 判断实体是否仍活跃（未被标记删除或死亡）。
func (w *World) Active(id EntityID) bool {
	record, ok := w.entities[id]
	return ok && record.active
}

// Kind 返回实体的类别（玩家/怪物）。
func (w *World) Kind(id EntityID) (EntityKind, bool) {
	record, ok := w.entities[id]
	if !ok {
		return EntityUnknown, false
	}
	return record.kind, true
}

// Transform 返回实体的位置和朝向副本。
func (w *World) Transform(id EntityID) (Transform, bool) {
	value, ok := w.transforms.get(id)
	if !ok {
		return Transform{}, false
	}
	return *value, true
}

// Movement 返回实体的运动状态副本。
func (w *World) Movement(id EntityID) (Movement, bool) {
	value, ok := w.movements.get(id)
	if !ok {
		return Movement{}, false
	}
	return *value, true
}

// Health 返回实体的生命值状态副本。
func (w *World) Health(id EntityID) (Health, bool) {
	value, ok := w.health.get(id)
	if !ok {
		return Health{}, false
	}
	return *value, true
}

// Combat 返回实体的战斗属性副本。
func (w *World) Combat(id EntityID) (Combat, bool) {
	value, ok := w.combat.get(id)
	if !ok {
		return Combat{}, false
	}
	return *value, true
}

// AI 返回实体的 AI 状态副本。
func (w *World) AI(id EntityID) (AI, bool) {
	value, ok := w.ai.get(id)
	if !ok {
		return AI{}, false
	}
	return *value, true
}

// PlayerState 返回玩家实体的网络元数据副本。
func (w *World) PlayerState(id EntityID) (PlayerState, bool) {
	value, ok := w.playerState.get(id)
	if !ok {
		return PlayerState{}, false
	}
	return *value, true
}

// Buffs 返回实体身上所有 Buff 的副本切片。
func (w *World) Buffs(id EntityID) ([]Buff, bool) {
	value, ok := w.buffs.get(id)
	if !ok {
		return nil, false
	}
	return append([]Buff(nil), *value...), true
}

// Events 返回本 Tick 产生的所有事件副本。
func (w *World) Events() []Event {
	return append([]Event(nil), w.events...)
}

// SetHealth 直接设置实体的生命值状态。
// 会自动钳制 Current ∈ [0, Maximum]，并根据 Current 更新 Dead 标志。
func (w *World) SetHealth(id EntityID, health Health) error {
	if !w.Exists(id) {
		return fmt.Errorf("%w: %d", ErrEntityNotFound, id)
	}
	if health.Maximum <= 0 {
		return fmt.Errorf("%w: maximum health must be positive", ErrInvalidCommand)
	}
	if health.Current < 0 {
		health.Current = 0
	}
	if health.Current > health.Maximum {
		health.Current = health.Maximum
	}
	health.Dead = health.Current == 0
	w.health.set(id, health)
	return nil
}

// AddPlayer 向世界添加一个玩家实体（仅可在 Step 外部调用）。
// 返回分配的 EntityID。
func (w *World) AddPlayer(spec PlayerSpec) (EntityID, error) {
	if w.stepping {
		return 0, ErrAlreadyStepping
	}
	return w.addPlayerNow(spec, false)
}

// AddMonster 向世界添加一个怪物实体（仅可在 Step 外部调用）。
// 返回分配的 EntityID。
func (w *World) AddMonster(spec MonsterSpec) (EntityID, error) {
	if w.stepping {
		return 0, ErrAlreadyStepping
	}
	return w.addMonsterNow(spec, false)
}

// addPlayerNow 立即创建玩家实体。emit 为 true 时产出 EventSpawned 事件。
func (w *World) addPlayerNow(spec PlayerSpec, emit bool) (EntityID, error) {
	if spec.PlayerID == "" {
		return 0, fmt.Errorf("%w: player ID is required", ErrInvalidCommand)
	}
	if _, exists := w.players[spec.PlayerID]; exists {
		return 0, fmt.Errorf("%w: %s", ErrPlayerAlreadyAdded, spec.PlayerID)
	}
	id, err := w.allocateEntity(EntityPlayer)
	if err != nil {
		return 0, err
	}

	maxSpeed := spec.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = w.cfg.PlayerMaxSpeed
	}
	health := normalizeHealth(spec.Health, w.cfg.DefaultPlayerHP)
	w.transforms.set(id, Transform{Position: spec.Position})
	w.movements.set(id, Movement{MaxSpeed: maxSpeed})
	w.health.set(id, health)
	w.combat.set(id, normalizeCombat(spec.Combat))
	w.playerState.set(id, PlayerState{PlayerID: spec.PlayerID})
	w.buffs.set(id, nil)
	w.players[spec.PlayerID] = id
	if emit {
		w.emit(Event{Type: EventSpawned, Entity: id, Position: spec.Position})
	}
	return id, nil
}

// addMonsterNow 立即创建怪物实体。emit 为 true 时产出 EventSpawned 事件。
// 对未指定的字段自动填入 cfg 中的默认值。
func (w *World) addMonsterNow(spec MonsterSpec, emit bool) (EntityID, error) {
	id, err := w.allocateEntity(EntityMonster)
	if err != nil {
		return 0, err
	}

	maxSpeed := spec.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = spec.AI.MoveSpeed
	}
	if maxSpeed <= 0 {
		maxSpeed = w.cfg.PlayerMaxSpeed / 2
	}
	ai := spec.AI
	if ai.AggroRange <= 0 {
		ai.AggroRange = w.cfg.DefaultAggroRange
	}
	if ai.MoveSpeed <= 0 {
		ai.MoveSpeed = maxSpeed
	}

	w.transforms.set(id, Transform{Position: spec.Position})
	w.movements.set(id, Movement{MaxSpeed: maxSpeed})
	w.health.set(id, normalizeHealth(spec.Health, w.cfg.DefaultMonsterHP))
	w.combat.set(id, normalizeCombat(spec.Combat))
	w.ai.set(id, ai)
	w.buffs.set(id, nil)
	w.lastSpawned = id
	if emit {
		w.emit(Event{Type: EventSpawned, Entity: id, Position: spec.Position})
	}
	return id, nil
}

// allocateEntity 分配下一个可用的 EntityID 并注册实体记录。
func (w *World) allocateEntity(kind EntityKind) (EntityID, error) {
	if len(w.entities) >= w.cfg.MaxEntities {
		return 0, ErrEntityLimit
	}
	w.nextEntityID++
	id := w.nextEntityID
	w.entities[id] = entityRecord{kind: kind, active: true}
	return id, nil
}

// normalizeHealth 用默认值填补 Health 的零值字段，确保数据合法。
func normalizeHealth(value Health, defaultMaximum float64) Health {
	if value.Maximum <= 0 {
		value.Maximum = defaultMaximum
	}
	if value.Current <= 0 && !value.Dead {
		value.Current = value.Maximum
	}
	if value.Current > value.Maximum {
		value.Current = value.Maximum
	}
	if value.Current <= 0 {
		value.Current = 0
		value.Dead = true
	}
	return value
}

// normalizeCombat 对 Combat 中的负数字段钳制为 0。
func normalizeCombat(value Combat) Combat {
	if value.Damage < 0 {
		value.Damage = 0
	}
	if value.Range < 0 {
		value.Range = 0
	}
	if value.Cooldown < 0 {
		value.Cooldown = 0
	}
	if value.Remaining < 0 {
		value.Remaining = 0
	}
	return value
}

// Step 实现 gameloop.World 接口，执行一个逻辑 Tick。
//
// 处理流程：
//  1. 校验 Tick 单调递增且 dt > 0
//  2. 清空上一帧的事件和攻击意图
//  3. 处理外部输入命令（移动、攻击、Buff、生成怪物等）
//  4. 按顺序执行各子系统：movement → ai → combat → buff → recovery → cleanup
//  5. 统一提交延迟的创建/删除操作
//
// 参考：更新方法模式——每一帧游戏循环遍历对象调用 update()。
// 这里不是遍历 Entity 对象，而是按领域依次执行 system，
// 每个 system 内部遍历相关实体的组件数据。
func (w *World) Step(ctx context.Context, tick uint64, dt time.Duration, commands []gameloop.Command) error {
	if tick <= w.tick || dt <= 0 {
		return fmt.Errorf("%w: tick=%d previous=%d delta=%s", ErrInvalidTick, tick, w.tick, dt)
	}
	if w.stepping {
		return ErrAlreadyStepping
	}
	w.stepping = true
	w.processingTick = tick
	defer func() {
		w.processingTick = 0
		w.stepping = false
	}()

	w.events = w.events[:0]
	w.attackIntents = w.attackIntents[:0]

	for i, command := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.handleCommand(command); err != nil {
			return fmt.Errorf("command %d: %w", i, err)
		}
	}

	tickContext := TickContext{Tick: tick, Delta: dt}
	for _, currentSystem := range w.systems {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := currentSystem.Update(ctx, w, tickContext); err != nil {
			return fmt.Errorf("tick %d system %s: %w", tick, currentSystem.Name(), err)
		}
	}
	if err := w.flushStructuralChanges(); err != nil {
		return fmt.Errorf("tick %d structural changes: %w", tick, err)
	}
	w.eventQueue.Dispatch()

	w.tick = tick
	return nil
}

// queueRemoval 将实体标记为非活跃并加入待删除队列。
// 实际删除在 flushStructuralChanges 中统一执行。
//
// 参考：更新方法模式——遍历期间不直接删除对象，
// 而是标记"死亡"后在遍历完成后统一移除。
func (w *World) queueRemoval(id EntityID) error {
	record, ok := w.entities[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrEntityNotFound, id)
	}
	if !record.active {
		return nil
	}
	record.active = false
	w.entities[id] = record
	w.pendingRemovals = append(w.pendingRemovals, id)
	return nil
}

// flushStructuralChanges 执行所有延迟的实体创建和删除。
// 在子系统全部执行完毕后调用，避免遍历期间修改集合。
func (w *World) flushStructuralChanges() error {
	for _, id := range w.pendingRemovals {
		w.removeEntityNow(id)
	}
	w.pendingRemovals = w.pendingRemovals[:0]

	for _, request := range w.pendingSpawns {
		if request.monster != nil {
			if _, err := w.addMonsterNow(*request.monster, true); err != nil {
				return err
			}
		}
	}
	w.pendingSpawns = w.pendingSpawns[:0]
	return nil
}

// removeEntityNow 立即从所有 store 中移除实体的全部数据。
// 由 flushStructuralChanges 调用，不产出事件。
func (w *World) removeEntityNow(id EntityID) {
	if _, ok := w.entities[id]; !ok {
		return
	}
	if state, exists := w.playerState.get(id); exists {
		delete(w.players, state.PlayerID)
	}
	w.transforms.remove(id)
	w.movements.remove(id)
	w.health.remove(id)
	w.combat.remove(id)
	w.ai.remove(id)
	w.playerState.remove(id)
	w.buffs.remove(id)
	delete(w.entities, id)
	w.emit(Event{Type: EventDespawned, Entity: id})
}

// emit 向本 Tick 的事件列表添加一个事件，自动填入当前 Tick 序号。
func (w *World) emit(event Event) {
	event.Tick = w.currentTick()
	w.events = append(w.events, event)
	w.eventQueue.Push(event)
}

// currentTick 返回当前"有效"的 Tick 序号。
// Step 执行中返回 processingTick，否则返回上次完成的 tick。
func (w *World) currentTick() uint64 {
	if w.stepping {
		return w.processingTick
	}
	return w.tick
}

// living 判断实体是否存活（活跃 + HP > 0 + 未标记死亡）。
func (w *World) living(id EntityID) bool {
	if !w.Active(id) {
		return false
	}
	health, ok := w.health.get(id)
	return ok && !health.Dead && health.Current > 0
}
