package gameloop

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrAlreadyRunning = errors.New("game loop already running")
	ErrNotRunning     = errors.New("game loop not running")
	ErrQueueFull      = errors.New("command queue full")
	ErrStopped        = errors.New("game loop stopped")
)

type scheduledCommand struct {
	command  Command
	sequence uint64
}

type commandHeap []scheduledCommand

func (h commandHeap) Len() int { return len(h) }

func (h commandHeap) Less(i, j int) bool {
	if h[i].command.ApplyTick != h[j].command.ApplyTick {
		return h[i].command.ApplyTick < h[j].command.ApplyTick
	}
	return h[i].sequence < h[j].sequence
}

func (h commandHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *commandHeap) Push(value any) {
	*h = append(*h, value.(scheduledCommand))
}

func (h *commandHeap) Pop() any {
	old := *h
	last := len(old) - 1
	item := old[last]
	old[last] = scheduledCommand{} // 避免堆底层数组长期引用 Command 内部对象。
	*h = old[:last]
	return item
}

type Loop struct {
	cfg       Config
	world     World
	publisher SnapshotPublisher
	observer  Observer

	commandCh chan Command
	stopCh    chan struct{}
	doneCh    chan struct{}
	stopOnce  sync.Once

	running atomic.Bool
	tick    atomic.Uint64

	// pendingCommands 统计 commandCh + futureCommands 中尚未交给 World.Step 的命令。
	// 不能只依赖 len(commandCh)，否则命令移入 futureCommands 后队列会失去容量上限。
	pendingCommands atomic.Int64

	// 以下字段只允许 Loop.Run 所在 goroutine 访问，不需要加锁。
	stepCommands   []Command
	futureCommands commandHeap
	nextSequence   uint64
}

func New(cfg Config, world World, publisher SnapshotPublisher, observer Observer) (*Loop, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if world == nil {
		return nil, errors.New("world is required")
	}
	if publisher == nil {
		publisher = NopPublisher{}
	}
	if observer == nil {
		observer = NopObserver{}
	}

	return &Loop{
		cfg:            cfg,
		world:          world,
		publisher:      publisher,
		observer:       observer,
		commandCh:      make(chan Command, cfg.CommandQueueSize),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		stepCommands:   make([]Command, 0, cfg.MaxCommandsPerTick),
		futureCommands: make(commandHeap, 0, cfg.CommandQueueSize),
	}, nil
}

// Submit 非阻塞提交命令。网络 IO 协程不可因单个房间拥塞而无限阻塞。
func (l *Loop) Submit(cmd Command) error {
	if !l.running.Load() {
		return ErrNotRunning
	}
	if cmd.ReceivedAt.IsZero() {
		cmd.ReceivedAt = time.Now()
	}

	// 先占用“总待处理队列”的名额，而不是只检查 channel 是否有空间。
	if !l.reserveCommandSlot() {
		l.observer.DroppedCommand("queue_full")
		return ErrQueueFull
	}

	select {
	case <-l.stopCh:
		l.pendingCommands.Add(-1)
		return ErrStopped
	case l.commandCh <- cmd:
		return nil
	default:
		l.pendingCommands.Add(-1)
		l.observer.DroppedCommand("queue_full")
		return ErrQueueFull
	}
}

func (l *Loop) reserveCommandSlot() bool {
	limit := int64(l.cfg.CommandQueueSize)
	for {
		current := l.pendingCommands.Load()
		if current >= limit {
			return false
		}
		if l.pendingCommands.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (l *Loop) Tick() uint64          { return l.tick.Load() }
func (l *Loop) Done() <-chan struct{} { return l.doneCh }

// Run 阻塞运行，通常每个房间一个 goroutine。
func (l *Loop) Run(ctx context.Context) error {
	if !l.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer l.running.Store(false)
	defer close(l.doneCh)

	tickDuration := time.Second / time.Duration(l.cfg.TickRate)
	snapshotEvery := uint64(0)
	if l.cfg.SnapshotRate > 0 {
		snapshotEvery = uint64(l.cfg.TickRate / l.cfg.SnapshotRate)
		if snapshotEvery == 0 {
			snapshotEvery = 1
		}
	}

	// 复用 Timer，避免主循环中反复 time.NewTimer。
	sleepTimer := time.NewTimer(time.Hour)
	stopAndDrainTimer(sleepTimer)
	defer stopAndDrainTimer(sleepTimer)

	previous := time.Now()
	lag := time.Duration(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopCh:
			return nil
		default:
		}

		now := time.Now()
		elapsed := now.Sub(previous) // Go 的 time.Time 携带单调时钟读数。
		previous = now
		if elapsed < 0 {
			elapsed = 0
		}
		if elapsed > l.cfg.MaxFrameElapsed {
			elapsed = l.cfg.MaxFrameElapsed
		}
		lag += elapsed

		catchUps := 0
		for lag >= tickDuration && catchUps < l.cfg.MaxCatchUpTicks {
			nextTick := l.tick.Add(1)
			commands := l.drainCommands(nextTick)

			started := time.Now()
			if err := l.safeStep(ctx, nextTick, tickDuration, commands); err != nil {
				l.observer.Error("step", err)
				return fmt.Errorf("tick %d: %w", nextTick, err)
			}
			cost := time.Since(started)
			catchUp := catchUps > 0
			l.observer.Tick(nextTick, cost, len(commands), catchUp)
			if cost > tickDuration {
				l.observer.SlowTick(nextTick, cost, tickDuration)
			}

			if snapshotEvery > 0 && nextTick%snapshotEvery == 0 {
				l.publishSnapshot(ctx, nextTick)
			}
			lag -= tickDuration
			catchUps++
		}

		if lag >= tickDuration &&
			catchUps >= l.cfg.MaxCatchUpTicks &&
			l.cfg.OverloadPolicy == OverloadDropLag {
			// 只保留不足一个 Tick 的余量，避免永远追赶历史积压。
			lag %= tickDuration
			l.observer.DroppedCommand("simulation_lag_dropped")
		}

		// 不使用 time.Ticker：Ticker 在暂停/GC 后可能产生难以控制的补发语义。
		sleep := l.cfg.IdleSleep
		if remaining := tickDuration - lag; remaining > 0 && remaining < sleep {
			sleep = remaining
		}
		if sleep <= 0 {
			continue
		}

		sleepTimer.Reset(sleep)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopCh:
			return nil
		case <-sleepTimer.C:
		}
	}
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (l *Loop) Stop(ctx context.Context) error {
	l.stopOnce.Do(func() {
		_ = l.world.Close()
		close(l.stopCh)
	})
	select {
	case <-l.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// drainCommands 返回的切片由 Loop 复用，只保证在下一次 drainCommands 调用前有效。
// World.Step 不应在返回后继续持有该切片。
func (l *Loop) drainCommands(tick uint64) []Command {
	result := l.stepCommands[:0]
	maxCommands := l.cfg.MaxCommandsPerTick

	// 优先执行已经到期的定时命令。
	for len(result) < maxCommands && len(l.futureCommands) > 0 {
		next := l.futureCommands[0]
		if next.command.ApplyTick > tick {
			break
		}
		item := heap.Pop(&l.futureCommands).(scheduledCommand)
		result = append(result, item.command)
	}

	// 只遍历本次调用开始时已经在 channel 中的消息，避免生产者持续写入导致循环不结束。
	queuedAtStart := len(l.commandCh)
	for scanned := 0; scanned < queuedAtStart && len(result) < maxCommands; scanned++ {
		select {
		case cmd := <-l.commandCh:
			if cmd.ApplyTick == 0 || cmd.ApplyTick <= tick {
				result = append(result, cmd)
				continue
			}

			// 未来命令只移动一次，之后一直留在最小堆中，直到 ApplyTick 到达。
			l.nextSequence++
			heap.Push(&l.futureCommands, scheduledCommand{
				command:  cmd,
				sequence: l.nextSequence,
			})
		default:
			// len(channel) 只是瞬时值；保留 default 防御未来出现其他消费者。
			scanned = queuedAtStart
		}
	}

	// 命令从待处理队列转交给同步的 World.Step 后，即可释放队列名额。
	l.pendingCommands.Add(-int64(len(result)))
	l.stepCommands = result
	return result
}

func (l *Loop) safeStep(parent context.Context, tick uint64, dt time.Duration, commands []Command) (err error) {
	defer func() {
		if r := recover(); r != nil {
			l.observer.Panic("step", r)
			err = fmt.Errorf("panic in world step: %v", r)
		}
	}()

	ctx := parent
	cancel := func() {}
	if l.cfg.TickTimeout > 0 {
		ctx, cancel = context.WithTimeout(parent, l.cfg.TickTimeout)
	}
	defer cancel()
	return l.world.Step(ctx, tick, dt, commands)
}

func (l *Loop) publishSnapshot(parent context.Context, tick uint64) {
	snapshot, err := l.world.Snapshot(parent, tick)
	if err != nil {
		l.observer.Error("snapshot", err)
		return
	}

	ctx := parent
	cancel := func() {}
	if l.cfg.PublishTimeout > 0 {
		ctx, cancel = context.WithTimeout(parent, l.cfg.PublishTimeout)
	}
	defer cancel()
	if err := l.publisher.Publish(ctx, tick, snapshot); err != nil {
		l.observer.Error("publish", err)
	}
}
