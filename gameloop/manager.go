package gameloop

import (
	"context"
	"errors"
	"sync"
)

// Manager 管理多个房间。房间内部单线程写状态，房间之间并行。
type Manager struct {
	mu    sync.RWMutex
	rooms map[string]*Loop
	wg    sync.WaitGroup
}

func NewManager() *Manager { return &Manager{rooms: make(map[string]*Loop)} }

func (m *Manager) Add(roomID string, loop *Loop) error {
	if roomID == "" || loop == nil {
		return errors.New("invalid room")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[roomID]; ok {
		return errors.New("room already exists")
	}
	m.rooms[roomID] = loop
	return nil
}

func (m *Manager) Start(ctx context.Context, roomID string) error {
	m.mu.RLock()
	loop := m.rooms[roomID]
	m.mu.RUnlock()
	if loop == nil {
		return errors.New("room not found")
	}
	m.wg.Add(1)
	go func() { defer m.wg.Done(); _ = loop.Run(ctx) }()
	return nil
}

func (m *Manager) Submit(roomID string, cmd Command) error {
	m.mu.RLock()
	loop := m.rooms[roomID]
	m.mu.RUnlock()
	if loop == nil {
		return errors.New("room not found")
	}
	return loop.Submit(cmd)
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	loops := make([]*Loop, 0, len(m.rooms))
	for _, loop := range m.rooms {
		loops = append(loops, loop)
	}
	m.mu.RUnlock()
	for _, loop := range loops {
		if err := loop.Stop(ctx); err != nil {
			return err
		}
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
