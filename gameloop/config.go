package gameloop

import (
	"errors"
	"time"
)

type OverloadPolicy int

const (
	// OverloadDropLag 丢弃过多累计时间，避免“死亡螺旋”。适合实时房间。
	OverloadDropLag OverloadPolicy = iota
	// OverloadSlowMotion 不丢逻辑 Tick，但房间会慢于真实时间。适合强一致模拟。
	OverloadSlowMotion
)

// Config Game Loop运行配置
//
// 用于控制服务器端游戏循环(Game Loop)的运行行为，包括：
// - Tick更新频率
// - 状态同步频率
// - 命令处理能力
// - 追帧策略
// - 超载保护
// - Tick执行超时控制
type Config struct {

	// TickRate 游戏逻辑更新频率
	//
	// 表示每秒执行多少次 World.Step()
	//
	// 例如：
	// 20 代表服务器每秒推进20个逻辑Tick
	// 即每个Tick间隔:
	// 1000ms / 20 = 50ms
	//
	// 常见配置：
	// - 棋牌类: 5~10
	// - MMO: 10~30
	// - FPS/动作游戏: 30~60
	TickRate int

	// SnapshotRate 状态快照同步频率
	//
	// 表示每秒向客户端或者其他服务发布多少次世界状态快照。
	//
	// 通常小于 TickRate:
	//
	// 例如:
	// TickRate=20
	// SnapshotRate=10
	//
	// 表示：
	// - 游戏逻辑每秒计算20次
	// - 网络同步每秒发送10次
	//
	// 用于降低网络带宽和序列化压力。
	SnapshotRate int

	// CommandQueueSize 输入命令队列容量
	//
	// 每个Game Loop拥有一个命令缓冲队列。
	//
	// 网络层收到玩家操作后不会直接修改世界状态，
	// 而是进入该队列等待下一个Tick处理。
	//
	// 队列满后应该触发：
	// - 丢弃低优先级命令
	// - 返回服务器繁忙
	// - 记录异常指标
	//
	// 防止异常客户端导致服务器阻塞。
	CommandQueueSize int

	// MaxCommandsPerTick 单个Tick最大处理命令数量
	//
	// 限制一次逻辑更新最多处理多少玩家输入。
	//
	// 防止：
	// 大量玩家请求
	// 恶意刷包
	// 网络重试风暴
	//
	// 导致单个Tick执行时间过长。
	//
	// 超出的命令会留到后续Tick继续处理。
	MaxCommandsPerTick int

	// MaxCatchUpTicks 最大追赶Tick数量
	//
	// 当服务器由于GC、CPU调度、系统暂停等原因导致
	// Game Loop落后真实时间时，会尝试补执行多个Tick。
	//
	// 例如:
	//
	// TickRate = 20
	// MaxCatchUpTicks = 4
	//
	// 一次最多补4个Tick。
	//
	// 防止出现：
	//
	// 延迟增加
	// -> 疯狂补Tick
	// -> CPU升高
	// -> 延迟继续增加
	// -> 死亡循环
	MaxCatchUpTicks int

	// MaxFrameElapsed 最大单次时间跨度
	//
	// 限制一次Loop循环允许累计的最大时间。
	//
	// 例如：
	//
	// 服务暂停5秒恢复，
	// 不允许一次性执行100个Tick追赶。
	//
	// 通常设置:
	//
	// 100ms ~ 500ms
	//
	// 用于保护：
	// - 长时间GC
	// - 容器暂停
	// - 虚拟机暂停
	// - 系统休眠恢复
	MaxFrameElapsed time.Duration

	// TickTimeout 单个Tick执行超时时间
	//
	// 用于检测一次World.Step()执行是否超过预算。
	//
	// Tick内部禁止执行:
	// - RPC
	// - MySQL查询
	// - Redis请求
	// - HTTP请求
	//
	// 只能执行:
	// - 内存计算
	// - 状态更新
	// - 游戏规则计算
	//
	// 超时后应该记录慢Tick指标。
	TickTimeout time.Duration

	// PublishTimeout 状态发布超时时间
	//
	// 控制Snapshot发布到外部系统的最大等待时间。
	//
	// 例如:
	// - WebSocket网关
	// - MQ
	// - Redis Stream
	// - NATS
	//
	// 注意:
	// 发布不能阻塞Game Loop。
	// 超时应该异步重试或者丢弃。
	PublishTimeout time.Duration

	// IdleSleep 空闲状态休眠时间
	//
	// 当Game Loop没有Tick任务或者等待调度时，
	// 使用该时间进行短暂休眠。
	//
	// 作用:
	// - 降低CPU空转
	// - 减少无意义调度
	//
	// 不建议设置过大，
	// 否则会影响Tick精度。
	IdleSleep time.Duration

	// OverloadPolicy 过载处理策略
	//
	// 当Game Loop无法追赶真实时间时，
	// 使用该策略决定如何处理。
	//
	// 常见策略:
	//
	// OverloadDropLag:
	//   丢弃累计延迟，让游戏快速恢复实时状态。
	//   适用于:
	//   - MMO
	//   - 实时竞技
	//   - 在线游戏
	//
	// OverloadSlowMotion:
	//   保留所有Tick，慢慢追赶。
	//   适用于:
	//   - 战斗回放
	//   - 离线模拟
	//   - 数据验证
	OverloadPolicy OverloadPolicy
}

func DefaultConfig() Config {
	return Config{
		TickRate:           20,
		SnapshotRate:       10,
		CommandQueueSize:   4096,
		MaxCommandsPerTick: 512,
		MaxCatchUpTicks:    4,
		MaxFrameElapsed:    250 * time.Millisecond,
		TickTimeout:        45 * time.Millisecond,
		PublishTimeout:     100 * time.Millisecond,
		IdleSleep:          time.Millisecond,
		OverloadPolicy:     OverloadDropLag,
	}
}

func (c Config) validate() error {
	if c.TickRate <= 0 || c.TickRate > 1000 {
		return errors.New("tick rate must be in [1,1000]")
	}
	if c.SnapshotRate < 0 || c.SnapshotRate > c.TickRate {
		return errors.New("snapshot rate must be in [0,tick rate]")
	}
	if c.CommandQueueSize <= 0 {
		return errors.New("command queue size must be positive")
	}
	if c.MaxCommandsPerTick <= 0 {
		return errors.New("max commands per tick must be positive")
	}
	if c.MaxCatchUpTicks <= 0 {
		return errors.New("max catch-up ticks must be positive")
	}
	if c.MaxFrameElapsed <= 0 {
		return errors.New("max frame elapsed must be positive")
	}
	return nil
}
