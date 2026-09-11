package world

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Sentinel errors for the update package.
var (
	// ErrInvalidConfig 配置参数校验失败。
	ErrInvalidConfig = errors.New("invalid update configuration")
	// ErrInvalidTick Tick 序号非法（如为 0）。
	ErrInvalidTick = errors.New("invalid tick")
	// ErrAlreadyStepping World 已在 Step 调用中（不可重入）。
	ErrAlreadyStepping = errors.New("world is already stepping")
	// ErrEntityLimit 实体数量已达 Config.MaxEntities 上限。
	ErrEntityLimit = errors.New("entity limit reached")
	// ErrEntityNotFound 指定 EntityID 在 World 中不存在。
	ErrEntityNotFound = errors.New("entity not found")
	// ErrPlayerNotFound 指定玩家尚未加入世界。
	ErrPlayerNotFound = errors.New("player not found")
	// ErrPlayerAlreadyAdded 玩家已在世界中，不允许重复加入。
	ErrPlayerAlreadyAdded = errors.New("player already added")
	// ErrUnsupportedCommand 命令 Payload 类型不被 Step 识别。
	ErrUnsupportedCommand = errors.New("unsupported command")
	// ErrInvalidCommand 命令参数非法（如 Target 为 0）。
	ErrInvalidCommand = errors.New("invalid command")
	// ErrBackendAlreadyAttached 表示 World 已绑定异步 Backend。
	ErrBackendAlreadyAttached = errors.New("backend already attached")
	// ErrBackendRequired 表示绑定的 Backend 为空。
	ErrBackendRequired = errors.New("backend is required")
	// ErrBackendNotAttached 表示 World 尚未绑定异步 Backend。
	ErrBackendNotAttached = errors.New("backend is not attached")
	// ErrBackendTaskIdentity 表示任务不属于当前 World、房间或实体版本。
	ErrBackendTaskIdentity = errors.New("backend task identity mismatch")
)

// WorldID 是一次 World 实例的唯一标识。World 重建后应使用新的 ID。
type WorldID uint64

// EntityID 是实体在 World 中的唯一标识。
// 由 World 在实体创建时分配，零值表示无效 ID。
type EntityID uint64

// EntityKind 区分实体的类别（玩家/怪物），用于快照和业务判断。
type EntityKind uint8

const (
	// EntityUnknown 未知类型，未初始化的零值。
	EntityUnknown EntityKind = iota
	// EntityPlayer 玩家实体，由 PlayerSpec 创建。
	EntityPlayer
	// EntityMonster 怪物实体，由 SpawnMonsterCommand 创建。
	EntityMonster
)

// Vec2 二维向量，用于位置、速度、朝向等计算。
// 所有方法返回新值，不修改原向量（值语义）。
type Vec2 struct {
	X float64
	Y float64
}

// Add 返回 v + other。
func (v Vec2) Add(other Vec2) Vec2 {
	return Vec2{X: v.X + other.X, Y: v.Y + other.Y}
}

// Sub 返回 v - other。
func (v Vec2) Sub(other Vec2) Vec2 {
	return Vec2{X: v.X - other.X, Y: v.Y - other.Y}
}

// Scale 返回 v 乘以标量 factor。
func (v Vec2) Scale(factor float64) Vec2 {
	return Vec2{X: v.X * factor, Y: v.Y * factor}
}

// Length 返回向量的欧几里得长度。
func (v Vec2) Length() float64 {
	return math.Hypot(v.X, v.Y)
}

// Normalize 返回同方向的单位向量。零向量返回零值。
func (v Vec2) Normalize() Vec2 {
	length := v.Length()
	if length == 0 {
		return Vec2{}
	}
	return v.Scale(1 / length)
}

// distance 计算 a 到 b 的欧几里得距离。
func distance(a, b Vec2) float64 {
	return a.Sub(b).Length()
}

// clampMagnitude 将向量的长度限制在 maximum 以内。
// 若 maximum <= 0 或向量为零，返回零向量。
func clampMagnitude(value Vec2, maximum float64) Vec2 {
	if maximum <= 0 {
		return Vec2{}
	}
	length := value.Length()
	if length <= maximum || length == 0 {
		return value
	}
	return value.Scale(maximum / length)
}

// Config 是 Update 系统的运行配置。
//
// 控制实体的数量上限、玩家和怪物的默认属性，
// 在创建 World 时传入并校验。
type Config struct {
	// WorldID 标识当前 World 实例；0 表示由 NewWorld 自动分配。
	WorldID WorldID
	// RoomID 沿用 gameloop.Manager 的字符串房间标识。
	RoomID string
	// MaxEntities 世界内最大实体数量（含玩家和怪物）。
	MaxEntities int
	// BackendResultQueueSize 是为当前 World 保留的异步结果容量。
	BackendResultQueueSize int
	// MaxBackendResultsPerTick 限制每个 Tick 最多应用多少个异步结果。
	MaxBackendResultsPerTick int
	// PlayerMaxSpeed 玩家移动的最大速度（单位/秒）。
	PlayerMaxSpeed float64
	// DefaultPlayerHP 新玩家实体的初始最大生命值。
	DefaultPlayerHP float64
	// DefaultMonsterHP 新怪物实体的初始最大生命值。
	DefaultMonsterHP float64
	// DefaultAggroRange 怪物默认仇恨范围，进入此范围的玩家会被锁定。
	DefaultAggroRange float64
}

// DefaultConfig 返回一套适合中等规模 MMO 场景的默认配置。
func DefaultConfig() Config {
	return Config{
		WorldID:                  0,
		RoomID:                   "default",
		MaxEntities:              4096,
		BackendResultQueueSize:   1024,
		MaxBackendResultsPerTick: 128,
		PlayerMaxSpeed:           8,
		DefaultPlayerHP:          100,
		DefaultMonsterHP:         50,
		DefaultAggroRange:        20,
	}
}

// validate 校验配置，失败返回 ErrInvalidConfig。
func (c Config) validate() error {
	if c.WorldID == 0 {
		return fmt.Errorf("%w: WorldID must be nonzero", ErrInvalidConfig)
	}
	if c.RoomID == "" {
		return fmt.Errorf("%w: RoomID is required", ErrInvalidConfig)
	}
	if c.MaxEntities <= 0 {
		return fmt.Errorf("%w: MaxEntities must be positive", ErrInvalidConfig)
	}
	if c.PlayerMaxSpeed <= 0 {
		return fmt.Errorf("%w: PlayerMaxSpeed must be positive", ErrInvalidConfig)
	}
	if c.DefaultPlayerHP <= 0 || c.DefaultMonsterHP <= 0 {
		return fmt.Errorf("%w: default health must be positive", ErrInvalidConfig)
	}
	if c.DefaultAggroRange < 0 {
		return fmt.Errorf("%w: DefaultAggroRange cannot be negative", ErrInvalidConfig)
	}
	if c.BackendResultQueueSize <= 0 {
		return fmt.Errorf("%w: BackendResultQueueSize must be positive", ErrInvalidConfig)
	}
	if c.MaxBackendResultsPerTick <= 0 {
		return fmt.Errorf("%w: MaxBackendResultsPerTick must be positive", ErrInvalidConfig)
	}
	return nil
}

// Transform 描述实体在空间中的位置和朝向。
// 朝向应为单位向量或零向量。
type Transform struct {
	Position Vec2 // 当前位置
	Facing   Vec2 // 面朝方向（单位向量）
}

// Movement 描述实体的运动状态。
type Movement struct {
	Velocity Vec2    // 当前速度（单位/秒）
	MaxSpeed float64 // 最大速率上限
}

// Health 描述实体的生命值状态。
type Health struct {
	Current           float64 // 当前生命值
	Maximum           float64 // 最大生命值
	RecoveryPerSecond float64 // 每秒自然回复量；0 表示不回复
	Dead              bool    // 是否已死亡
}

// Combat 描述实体的战斗属性。
type Combat struct {
	Damage    float64       // 每次攻击的基础伤害
	Range     float64       // 攻击范围
	Cooldown  time.Duration // 攻击冷却时间
	Remaining time.Duration // 距离下次可攻击的剩余时间；每 Tick 扣减
}

// AI 描述怪物的自主行为参数。
type AI struct {
	Target     EntityID // 当前追击目标；0 表示无目标
	AggroRange float64  // 仇恨触发范围
	MoveSpeed  float64  // 追击时的移动速度
}

// PlayerState 是挂在玩家实体上的网络层元数据。
// 用于命令去重和乱序保护（按 LastSeq 递增）。
type PlayerState struct {
	PlayerID string
	LastSeq  uint64 // 该玩家已处理的最后一条命令序号
}

// BuffKind 表示 Buff 的类型。
//
// 每种 Buff 在 Tick 更新时有不同的处理逻辑：
//   - BuffDamageOverTime 按周期扣减生命值
//   - BuffAttackPower 修改实体的攻击力
//   - BuffMoveSpeed 修改实体的移动速度
type BuffKind uint8

const (
	// BuffUnknown 未指定类型，不应在运行时出现。
	BuffUnknown BuffKind = iota
	// BuffDamageOverTime 持续伤害，每隔 Period 对目标造成 Magnitude 点伤害。
	BuffDamageOverTime
	// BuffAttackPower 攻击力增益，将 Combat.Damage 乘以 (1 + Magnitude)。
	BuffAttackPower
	// BuffMoveSpeed 移速增益，将 Movement.MaxSpeed 乘以 (1 + Magnitude)。
	BuffMoveSpeed
)

// Buff 描述一个施加在实体上的临时效果。
//
// Duration 内 Buff 生效，Period > 0 时按周期触发效果（如每 1s 扣血一次），
// Period == 0 表示在整个 Duration 期间持续生效（如攻击力提升）。
type Buff struct {
	ID        string        // Buff 实例的唯一标识
	Kind      BuffKind      // Buff 类型
	Source    EntityID      // 施加者实体 ID
	Duration  time.Duration // 总持续时间
	Remaining time.Duration // 剩余时间；归零后 Buff 过期
	Period    time.Duration // 触发周期；0 表示持续生效
	UntilNext time.Duration // 距离下次触发的剩余时间
	Magnitude float64       // 效果量级（伤害值或增益系数）
}

// PlayerSpec 定义创建玩家实体时所需的各项初始属性。
type PlayerSpec struct {
	PlayerID string  // 玩家外部 ID（与网关/账号关联）
	Position Vec2    // 出生点位置
	MaxSpeed float64 // 玩家移动速度上限
	Health   Health  // 初始生命值配置
	Combat   Combat  // 战斗属性
}

// MonsterSpec 定义创建怪物实体时所需的各项初始属性。
type MonsterSpec struct {
	Position Vec2    // 出生点位置
	MaxSpeed float64 // 移动速度上限
	Health   Health  // 初始生命值
	Combat   Combat  // 战斗属性
	AI       AI      // AI 行为参数（仇恨范围、追击速度等）
}

// MoveCommand 表示玩家/实体的移动指令。
type MoveCommand struct {
	Velocity Vec2 // 期望的移动速度向量
}

// AttackCommand 表示攻击指令，由攻击者发起。
type AttackCommand struct {
	Target EntityID // 攻击目标实体 ID
}

// ApplyBuffCommand 表示向目标施加 Buff 的指令。
type ApplyBuffCommand struct {
	Target EntityID // 目标实体 ID
	Buff   Buff     // 要施加的 Buff
}

// SpawnMonsterCommand 表示生成新怪物的指令。
type SpawnMonsterCommand struct {
	Spec MonsterSpec // 怪物的初始属性
}

// DespawnCommand 表示移除实体的指令（玩家下线或怪物被清理）。
type DespawnCommand struct {
	Entity EntityID // 待移除的实体 ID
}

// EventType 枚举事件种类，供快照中标识每个 Event 的语义。
type EventType string

const (
	// EventSpawned 实体被创建（玩家加入或怪物生成）。
	EventSpawned EventType = "spawned"
	// EventMoved 实体位置发生变化。
	EventMoved EventType = "moved"
	// EventAttack 实体发起了一次攻击。
	EventAttack EventType = "attack"
	// EventDamage 实体受到伤害（可能是持续伤害或攻击伤害）。
	EventDamage EventType = "damage"
	// EventBuffApplied 实体被施加了一个 Buff。
	EventBuffApplied EventType = "buff_applied"
	// EventBuffExpired 实体身上的 Buff 已过期移除。
	EventBuffExpired EventType = "buff_expired"
	// EventRecovered 实体自然回复了生命值。
	EventRecovered EventType = "recovered"
	// EventDeath 实体生命值归零死亡。
	EventDeath EventType = "death"
	// EventDespawned 实体被移除（玩家下线或怪物清理）。
	EventDespawned EventType = "despawned"
)

// Event 描述一个 Tick 内发生的事件，挂载在 Snapshot 中供客户端或日志使用。
type Event struct {
	Tick     uint64    // 发生时的逻辑 Tick 序号
	Type     EventType // 事件类型
	Entity   EntityID  // 主体实体
	Target   EntityID  // 目标实体（攻击/伤害/Buff 等）
	Amount   float64   // 数值（伤害量/回复量等）
	Position Vec2      // 事件发生位置
	BuffID   string    // 关联的 Buff ID（若有）
}

// EntitySnapshot 是单个实体在某个 Tick 的只读快照。
type EntitySnapshot struct {
	ID        EntityID   // 实体 ID
	Epoch     uint32     // 实体版本，用于过滤异步任务的旧结果
	Kind      EntityKind // 实体类别
	PlayerID  string     // 玩家外部 ID；非玩家实体为空
	Position  Vec2       // 当前位置
	Facing    Vec2       // 当前朝向
	Velocity  Vec2       // 当前速度
	Health    float64    // 当前生命值
	MaxHealth float64    // 最大生命值
	Dead      bool       // 是否死亡
	Target    EntityID   // 当前目标（AI 锁定或玩家选中）
	Buffs     []Buff     // 身上的 Buff 列表
}

// Snapshot 是某一 Tick 结束后的世界只读快照。
// 并发安全：由 World.Step 在单 goroutine 中生成，外部只读。
type Snapshot struct {
	WorldID  WorldID          // 产生快照的 World 实例
	RoomID   string           // 产生快照的房间
	Tick     uint64           // 逻辑 Tick 序号
	Entities []EntitySnapshot // 本帧所有活跃实体的快照
	Events   []Event          // 本帧产生的所有事件
}
