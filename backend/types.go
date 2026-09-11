// Package backend provides bounded asynchronous task execution for Worlds.
package backend

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrClosed          = errors.New("backend executor closed")
	ErrInvalidConfig   = errors.New("invalid backend configuration")
	ErrInvalidTask     = errors.New("invalid backend task")
	ErrTaskQueueFull   = errors.New("backend task queue full")
	ErrResultQueueFull = errors.New("backend result queue full")
	ErrWorldExists     = errors.New("backend world mailbox already exists")
	ErrWorldNotFound   = errors.New("backend world mailbox not found")
)

type TaskID uint64
type TaskKind uint16
type WorldID uint64
type EntityID uint64

type Config struct {
	WorkerCount   int
	TaskQueueSize int
}

func DefaultConfig() Config {
	return Config{
		WorkerCount:   8,
		TaskQueueSize: 4096,
	}
}

func (c Config) validate() error {
	if c.WorkerCount <= 0 {
		return fmt.Errorf("%w: WorkerCount must be positive", ErrInvalidConfig)
	}
	if c.TaskQueueSize <= 0 {
		return fmt.Errorf("%w: TaskQueueSize must be positive", ErrInvalidConfig)
	}
	return nil
}

type TaskMeta struct {
	TaskID      TaskID
	Kind        TaskKind
	WorldID     WorldID
	RoomID      string
	EntityID    EntityID
	EntityEpoch uint32
	SubmitTick  uint64
}

func (m TaskMeta) validate() error {
	if m.Kind == 0 {
		return fmt.Errorf("%w: TaskKind must be nonzero", ErrInvalidTask)
	}
	if m.WorldID == 0 {
		return fmt.Errorf("%w: WorldID must be nonzero", ErrInvalidTask)
	}
	if m.RoomID == "" {
		return fmt.Errorf("%w: RoomID is required", ErrInvalidTask)
	}
	if m.EntityID != 0 && m.EntityEpoch == 0 {
		return fmt.Errorf("%w: EntityEpoch is required for an entity task", ErrInvalidTask)
	}
	return nil
}

type ResultStatus uint8

const (
	ResultSuccess ResultStatus = iota
	ResultFailed
	ResultTimeout
	ResultOutcomeUnknown
	ResultCanceled
	ResultRejected
	ResultPanic
)

type TaskPolicy struct {
	Timeout         time.Duration
	CancelOnTimeout bool
	TimeoutStatus   ResultStatus
}

func (p TaskPolicy) normalized() TaskPolicy {
	if p.TimeoutStatus != ResultTimeout && p.TimeoutStatus != ResultOutcomeUnknown {
		p.TimeoutStatus = ResultTimeout
	}
	return p
}

type Task interface {
	Meta() TaskMeta
	Policy() TaskPolicy
	Execute(context.Context) (any, error)
}

type Result struct {
	Meta       TaskMeta
	Status     ResultStatus
	Data       any
	Err        error
	StartedAt  time.Time
	FinishedAt time.Time
}

// Service is the boundary used by World integration code.
type Service interface {
	RegisterWorld(worldID WorldID, resultQueueSize int) error
	UnregisterWorld(worldID WorldID)
	Submit(task Task) (TaskID, error)
	Cancel(taskID TaskID) bool
	DrainResults(worldID WorldID, max int) []Result
}
