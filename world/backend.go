package world

import "mmo/backend"

// BackendResultHandler applies one immutable Backend result inside World.Step.
// The handler may mutate World because it always runs in the World goroutine.
type BackendResultHandler func(world *World, result backend.Result)

// AttachBackend registers this World's result mailbox during setup.
func (w *World) AttachBackend(service backend.Service) error {
	if service == nil {
		return ErrBackendRequired
	}
	if w.stepping {
		return ErrAlreadyStepping
	}
	if w.backend != nil {
		return ErrBackendAlreadyAttached
	}
	if err := service.RegisterWorld(backend.WorldID(w.cfg.WorldID), w.cfg.BackendResultQueueSize); err != nil {
		return err
	}
	w.backend = service
	return nil
}

// DetachBackend unregisters this World and cancels its cooperative tasks.
func (w *World) DetachBackend() {
	if w.backend == nil {
		return
	}
	w.backend.UnregisterWorld(backend.WorldID(w.cfg.WorldID))
	w.backend = nil
	clear(w.pendingBackendTasks)
}

// RegisterBackendResultHandler binds a task kind to World-owned result logic.
// Register handlers before the game loop starts.
func (w *World) RegisterBackendResultHandler(kind backend.TaskKind, handler BackendResultHandler) {
	if kind == 0 || handler == nil {
		return
	}
	w.backendHandlers[kind] = handler
}

// SubmitBackendTask validates ownership before forwarding asynchronous work.
// Runtime callers should invoke it from the World goroutine.
func (w *World) SubmitBackendTask(task backend.Task) (backend.TaskID, error) {
	if w.backend == nil {
		return 0, ErrBackendNotAttached
	}
	if task == nil {
		return 0, backend.ErrInvalidTask
	}
	meta := task.Meta()
	if meta.WorldID != backend.WorldID(w.cfg.WorldID) || meta.RoomID != w.cfg.RoomID {
		return 0, ErrBackendTaskIdentity
	}
	if meta.EntityID != 0 {
		record, ok := w.entities[EntityID(meta.EntityID)]
		if !ok || !record.active || record.epoch != meta.EntityEpoch {
			return 0, ErrBackendTaskIdentity
		}
	}
	taskID, err := w.backend.Submit(task)
	if err != nil {
		return 0, err
	}
	w.pendingBackendTasks[taskID] = struct{}{}
	return taskID, nil
}

func (w *World) consumeBackendResults() {
	if w.backend == nil {
		return
	}
	results := w.backend.DrainResults(
		backend.WorldID(w.cfg.WorldID),
		w.cfg.MaxBackendResultsPerTick,
	)
	for _, result := range results {
		if !w.acceptBackendResult(result) {
			continue
		}
		handler := w.backendHandlers[result.Meta.Kind]
		if handler != nil {
			handler(w, result)
		}
	}
}

func (w *World) acceptBackendResult(result backend.Result) bool {
	if result.Meta.TaskID == 0 {
		return false
	}
	if _, pending := w.pendingBackendTasks[result.Meta.TaskID]; !pending {
		return false
	}
	// A terminal Result consumes the task even when its identity is stale.
	// This also prevents duplicate delivery from invoking a handler twice.
	delete(w.pendingBackendTasks, result.Meta.TaskID)

	if result.Meta.WorldID != backend.WorldID(w.cfg.WorldID) ||
		result.Meta.RoomID != w.cfg.RoomID {
		return false
	}
	if result.Meta.EntityID == 0 {
		return true
	}
	record, ok := w.entities[EntityID(result.Meta.EntityID)]
	return ok && record.active && record.epoch == result.Meta.EntityEpoch
}
