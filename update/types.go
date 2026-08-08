package update

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrInvalidConfig      = errors.New("invalid update configuration")
	ErrInvalidTick        = errors.New("invalid tick")
	ErrAlreadyStepping    = errors.New("world is already stepping")
	ErrEntityLimit        = errors.New("entity limit reached")
	ErrEntityNotFound     = errors.New("entity not found")
	ErrPlayerNotFound     = errors.New("player not found")
	ErrPlayerAlreadyAdded = errors.New("player already added")
	ErrUnsupportedCommand = errors.New("unsupported command")
	ErrInvalidCommand     = errors.New("invalid command")
)

type EntityID uint64

type EntityKind uint8

const (
	EntityUnknown EntityKind = iota
	EntityPlayer
	EntityMonster
)

type Vec2 struct {
	X float64
	Y float64
}

func (v Vec2) Add(other Vec2) Vec2 {
	return Vec2{X: v.X + other.X, Y: v.Y + other.Y}
}

func (v Vec2) Sub(other Vec2) Vec2 {
	return Vec2{X: v.X - other.X, Y: v.Y - other.Y}
}

func (v Vec2) Scale(factor float64) Vec2 {
	return Vec2{X: v.X * factor, Y: v.Y * factor}
}

func (v Vec2) Length() float64 {
	return math.Hypot(v.X, v.Y)
}

func (v Vec2) Normalize() Vec2 {
	length := v.Length()
	if length == 0 {
		return Vec2{}
	}
	return v.Scale(1 / length)
}

func distance(a, b Vec2) float64 {
	return a.Sub(b).Length()
}

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

type Config struct {
	MaxEntities       int
	PlayerMaxSpeed    float64
	DefaultPlayerHP   float64
	DefaultMonsterHP  float64
	DefaultAggroRange float64
}

func DefaultConfig() Config {
	return Config{
		MaxEntities:       4096,
		PlayerMaxSpeed:    8,
		DefaultPlayerHP:   100,
		DefaultMonsterHP:  50,
		DefaultAggroRange: 20,
	}
}

func (c Config) validate() error {
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
	return nil
}

type Transform struct {
	Position Vec2
	Facing   Vec2
}

type Movement struct {
	Velocity Vec2
	MaxSpeed float64
}

type Health struct {
	Current           float64
	Maximum           float64
	RecoveryPerSecond float64
	Dead              bool
}

type Combat struct {
	Damage    float64
	Range     float64
	Cooldown  time.Duration
	Remaining time.Duration
}

type AI struct {
	Target     EntityID
	AggroRange float64
	MoveSpeed  float64
}

type PlayerState struct {
	PlayerID string
	LastSeq  uint64
}

type BuffKind uint8

const (
	BuffUnknown BuffKind = iota
	BuffDamageOverTime
	BuffAttackPower
	BuffMoveSpeed
)

type Buff struct {
	ID        string
	Kind      BuffKind
	Source    EntityID
	Duration  time.Duration
	Remaining time.Duration
	Period    time.Duration
	UntilNext time.Duration
	Magnitude float64
}

type PlayerSpec struct {
	PlayerID string
	Position Vec2
	MaxSpeed float64
	Health   Health
	Combat   Combat
}

type MonsterSpec struct {
	Position Vec2
	MaxSpeed float64
	Health   Health
	Combat   Combat
	AI       AI
}

type MoveCommand struct {
	Velocity Vec2
}

type AttackCommand struct {
	Target EntityID
}

type ApplyBuffCommand struct {
	Target EntityID
	Buff   Buff
}

type SpawnMonsterCommand struct {
	Spec MonsterSpec
}

type DespawnCommand struct {
	Entity EntityID
}

type EventType string

const (
	EventSpawned     EventType = "spawned"
	EventMoved       EventType = "moved"
	EventAttack      EventType = "attack"
	EventDamage      EventType = "damage"
	EventBuffApplied EventType = "buff_applied"
	EventBuffExpired EventType = "buff_expired"
	EventRecovered   EventType = "recovered"
	EventDeath       EventType = "death"
	EventDespawned   EventType = "despawned"
)

type Event struct {
	Tick     uint64
	Type     EventType
	Entity   EntityID
	Target   EntityID
	Amount   float64
	Position Vec2
	BuffID   string
}

type EntitySnapshot struct {
	ID        EntityID
	Kind      EntityKind
	PlayerID  string
	Position  Vec2
	Facing    Vec2
	Velocity  Vec2
	Health    float64
	MaxHealth float64
	Dead      bool
	Target    EntityID
	Buffs     []Buff
}

type Snapshot struct {
	Tick     uint64
	Entities []EntitySnapshot
	Events   []Event
}
