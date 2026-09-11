package world

const MaxKeepCapacity = 8192

// EventHandler 处理已经发生的 World 事件。
// Handler 不应该直接修改 World 外部状态；如需改变游戏状态，
// 应通过 Command 或下一轮 Tick 的系统逻辑完成。
type EventHandler func(Event)

// EventQueue 是 World 内部事件延迟分发队列。
// Event 仍然保留在 Snapshot 中供客户端/外部消费，
// Queue 主要用于解耦 World 内部系统之间的通知关系。
//
// 约定：
//   - Push 只在 World goroutine 调用。
//   - Dispatch 只在 Tick 生命周期内调用。
//   - Dispatch 开始时只处理当前批次，新产生事件留到下一批。
type EventQueue struct {
	// 等待处理事件
	pending []Event

	// 当前正在处理事件
	//
	// 与 pending 分离，避免 handler 内 Push
	// 覆盖当前正在 Dispatch 的事件。
	processing []Event

	// 事件处理器
	handlers map[EventType][]EventHandler

	capacity int
}

func NewEventQueue(capacity int) *EventQueue {
	if capacity < 0 {
		capacity = 0
	}

	return &EventQueue{
		pending:    make([]Event, 0, capacity),
		processing: make([]Event, 0, capacity),
		handlers:   make(map[EventType][]EventHandler),
		capacity:   capacity,
	}
}

// Push 添加事件。
// 新事件不会在当前 Dispatch 中执行。
func (q *EventQueue) Push(event Event) {
	q.pending = append(q.pending, event)
}

func (q *EventQueue) Subscribe(eventType EventType, handler EventHandler) {
	if handler == nil {
		return
	}

	q.handlers[eventType] = append(q.handlers[eventType], handler)
}

// Dispatch 返回本轮分发数量。
//
// 处理规则:
//
// Tick N:
//
// pending:
//
//	A
//	B
//
// # Dispatch
//
// processing:
//
//	A
//	B
//
// handler(A)
//
//	Push(C)
//
// pending:
//
//	C
//
// Tick N:
//
// 只处理 A B
//
// Tick N+1:
//
// 处理 C
//
// 避免:
//
// A -> B -> A
//
// 无限事件循环。
func (q *EventQueue) Dispatch() int {
	if len(q.pending) == 0 {
		return 0
	}

	/*
	   交换buffer
	   原:
	   pending
	   [A][B][C]

	   processing
	   []

	   后:
	   pending
	   []

	   processing
	   [A][B][C]
	*/
	q.processing, q.pending = q.pending, q.processing

	// 清空新的pending
	q.pending = q.pending[:0]

	count := len(q.processing)
	for _, event := range q.processing {
		handlers, ok := q.handlers[event.Type]
		if !ok {
			continue
		}

		for _, handler := range handlers {
			handler(event)
		}
	}

	// 当前批次处理结束
	q.processing = q.processing[:0]

	// 释放引用
	clear(q.processing)

	// 防止异常峰值长期占用
	if cap(q.processing) > MaxKeepCapacity {
		q.processing = make([]Event, 0, q.capacity)
	} else {
		q.processing = q.processing[:0]
	}

	return count
}
