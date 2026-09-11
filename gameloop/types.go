package gameloop

import (
	"context"
	"time"
)

// Command 是外部网络层提交给游戏循环的输入。
// ApplyTick=0 表示由循环分配到下一次 Tick；非零值可用于帧同步/回放。
type Command struct {
	PlayerID   string
	Seq        uint64
	ApplyTick  uint64
	Payload    any
	ReceivedAt time.Time
}

// World 是业务世界接口。所有状态写操作只允许发生在 Step 内，
// 从而避免多协程直接修改房间状态。
type World interface {
	Step(ctx context.Context, tick uint64, dt time.Duration, commands []Command) error
	Snapshot(ctx context.Context, tick uint64) (any, error)
	Close() error
}

// SnapshotPublisher 将只读快照发送给网关、广播层或持久化层。
type SnapshotPublisher interface {
	Publish(ctx context.Context, tick uint64, snapshot any) error
}

type NopPublisher struct{}

func (NopPublisher) Publish(context.Context, uint64, any) error { return nil }

// Observer 用于接入 Prometheus、日志和链路追踪。
type Observer interface {
	Tick(tick uint64, cost time.Duration, commands int, catchUp bool)
	SlowTick(tick uint64, cost time.Duration, budget time.Duration)
	DroppedCommand(reason string)
	Error(stage string, err error)
	Panic(stage string, recovered any)
}

type NopObserver struct{}

func (NopObserver) Tick(uint64, time.Duration, int, bool)         {}
func (NopObserver) SlowTick(uint64, time.Duration, time.Duration) {}
func (NopObserver) DroppedCommand(string)                         {}
func (NopObserver) Error(string, error)                           {}
func (NopObserver) Panic(string, any)                             {}
