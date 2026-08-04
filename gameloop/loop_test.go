package gameloop

import (
	"errors"
	"testing"
	"time"
)

func newSchedulerTestLoop(queueSize, maxPerTick int) *Loop {
	return &Loop{
		cfg: Config{
			CommandQueueSize:   queueSize,
			MaxCommandsPerTick: maxPerTick,
		},
		commandCh:      make(chan Command, queueSize),
		stopCh:         make(chan struct{}),
		observer:       NopObserver{},
		stepCommands:   make([]Command, 0, maxPerTick),
		futureCommands: make(commandHeap, 0, queueSize),
	}
}

func markedCommand(applyTick uint64, marker int64) Command {
	return Command{
		ApplyTick:  applyTick,
		ReceivedAt: time.Unix(0, marker),
	}
}

func enqueueDirect(l *Loop, cmds ...Command) {
	for _, cmd := range cmds {
		l.pendingCommands.Add(1)
		l.commandCh <- cmd
	}
}

func TestDrainCommandsDefersFutureCommandUntilApplyTick(t *testing.T) {
	l := newSchedulerTestLoop(8, 8)
	enqueueDirect(l, markedCommand(20, 1))

	if got := l.drainCommands(10); len(got) != 0 {
		t.Fatalf("tick 10 returned %d commands, want 0", len(got))
	}
	if got := l.pendingCommands.Load(); got != 1 {
		t.Fatalf("pending after defer = %d, want 1", got)
	}

	got := l.drainCommands(20)
	if len(got) != 1 || got[0].ReceivedAt.UnixNano() != 1 {
		t.Fatalf("tick 20 returned %#v, want marker 1", got)
	}
	if got := l.pendingCommands.Load(); got != 0 {
		t.Fatalf("pending after execution = %d, want 0", got)
	}
}

func TestDrainCommandsPreservesOrderForSameApplyTick(t *testing.T) {
	l := newSchedulerTestLoop(8, 8)
	enqueueDirect(l,
		markedCommand(20, 1),
		markedCommand(20, 2),
		markedCommand(20, 3),
	)

	_ = l.drainCommands(10)
	got := l.drainCommands(20)
	if len(got) != 3 {
		t.Fatalf("got %d commands, want 3", len(got))
	}
	for i, cmd := range got {
		want := int64(i + 1)
		if cmd.ReceivedAt.UnixNano() != want {
			t.Fatalf("command %d marker = %d, want %d", i, cmd.ReceivedAt.UnixNano(), want)
		}
	}
}

func TestDrainCommandsKeepsReadyBacklogAcrossTicks(t *testing.T) {
	l := newSchedulerTestLoop(8, 2)
	enqueueDirect(l,
		markedCommand(0, 1),
		markedCommand(0, 2),
		markedCommand(0, 3),
	)

	first := l.drainCommands(1)
	if len(first) != 2 || first[0].ReceivedAt.UnixNano() != 1 || first[1].ReceivedAt.UnixNano() != 2 {
		t.Fatalf("first drain = %#v, want markers 1,2", first)
	}

	// drainCommands 返回的是 Loop 复用的临时切片，下一次调用会覆盖它。
	second := l.drainCommands(2)
	if len(second) != 1 || second[0].ReceivedAt.UnixNano() != 3 {
		t.Fatalf("second drain = %#v, want marker 3", second)
	}
}

func TestSubmitCountsCommandsMovedOutOfChannelAgainstQueueLimit(t *testing.T) {
	l := newSchedulerTestLoop(1, 1)
	l.running.Store(true)

	if err := l.Submit(markedCommand(100, 1)); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	if got := l.drainCommands(1); len(got) != 0 {
		t.Fatalf("future command executed early: %#v", got)
	}
	if len(l.commandCh) != 0 {
		t.Fatalf("channel length = %d, want 0 after scheduling", len(l.commandCh))
	}

	if err := l.Submit(markedCommand(100, 2)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second Submit() error = %v, want ErrQueueFull", err)
	}
}
