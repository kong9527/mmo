# MMO Backend Async Task Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a bounded in-process Backend worker pool and integrate validated asynchronous Results into the existing World tick.

**Architecture:** `backend.Executor` accepts typed Tasks through a bounded queue, executes them in fixed workers with per-task timeout policy, and routes exactly one Result into a reserved per-World mailbox slot. World drains and validates mailbox Results at the start of a later `Step`, so only the World goroutine mutates entities.

**Tech Stack:** Go 1.25 standard library, channels, context, sync/atomic, existing `mmo/world` and `mmo/gameloop` packages.

**Spec:** `docs/superpowers/specs/2026-09-11-backend-async-task-design.md`

## Global Constraints

- Keep `gameloop.Command`; asynchronous work is named Task and Result.
- Use one general worker pool for IO and CPU work in the first version.
- Keep existing public constructors working.
- Make only minimal edits to existing files.
- Backend must not mutate World state or retain World/Entity pointers.
- Process Results only at the beginning of a World tick and cap work per tick.
- Preserve the uploaded repository's existing uncommitted changes.

---

### Task 1: Backend executor and mailbox

**Files:**
- Create: `backend/types.go`
- Create: `backend/executor.go`
- Test: `backend/executor_test.go`

**Interfaces:**
- Produces: `Task`, `TaskMeta`, `TaskPolicy`, `Result`, `Executor`, `New`, `RegisterWorld`, `UnregisterWorld`, `Submit`, `Cancel`, `DrainResults`, `Close`.

- [x] Write tests for successful execution, queue rejection, reserved mailbox capacity, timeout, outcome-unknown timeout, panic recovery, cancellation, and world unregistration.
- [ ] Run `go test ./backend` and verify failure because the package API is missing.
- [x] Implement the smallest fixed worker pool and per-World mailbox that satisfies the tests.
- [ ] Run `go test ./backend` and verify all Backend tests pass.

### Task 2: Add World and Entity identity

**Files:**
- Modify: `world/types.go`
- Modify: `world/world.go`
- Modify: `world/snapshot.go`
- Test: `world/world_test.go`

**Interfaces:**
- Consumes: `backend.WorldID`, `backend.EntityID`.
- Produces: `World.ID()`, `World.RoomID()`, `World.EntityEpoch(EntityID)`, snapshot identity fields.

- [x] Write tests asserting configured WorldID/RoomID and nonzero entity epoch appear in World queries and snapshots.
- [ ] Run `go test ./world` and verify failure because identity fields and methods are missing.
- [x] Add identity configuration and entity epoch to existing records and snapshot generation without changing entity update behavior.
- [ ] Run `go test ./world` and verify identity tests and existing tests pass.

### Task 3: Connect Backend Results to World ticks

**Files:**
- Create: `world/backend.go`
- Modify: `world/world.go`
- Modify: `world/types.go`
- Test: `world/backend_test.go`

**Interfaces:**
- Consumes: `backend.Executor`, `backend.Result`, `backend.TaskKind`.
- Produces: `AttachBackend`, `DetachBackend`, `RegisterBackendResultHandler` and bounded result draining in `World.Step`.

- [x] Write tests proving a completed result is applied only during a subsequent Step, mismatched entity epoch is discarded, and the per-tick drain limit is respected.
- [ ] Run `go test ./world` and verify failure because the bridge API is missing.
- [x] Implement the bridge and call it before ordinary command handling in `World.Step`.
- [ ] Run `go test ./world` and verify all World tests pass.

### Task 4: Add an end-to-end usage example

**Files:**
- Create: `example/backend_task.go`
- Modify: `example/mmo_world.go`
- Test: `example/backend_task_test.go`

**Interfaces:**
- Consumes: Backend executor, World identity, entity epoch and handler registration.
- Produces: a context-aware `LoadPlayerProfileTask` example and `NewMMOLoopWithBackend` wiring helper.

- [x] Write an example test that submits a delayed task and observes the handler update only through a World Step.
- [ ] Run `go test ./example` and verify failure before adding the example implementation.
- [x] Implement the example using immutable IDs and payload values only.
- [ ] Run `go test ./example` and verify the example and existing API tests pass.

### Task 5: Verify and package

**Files:**
- Modify: `README.md`
- Create: updated `mmo(2).zip`

- [x] Document startup, submission, timeout status, and World result handling.
- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test -race ./...` and `go vet ./...`.
- [x] Review `git diff --ignore-space-at-eol` to verify changes are limited to Backend integration.
- [ ] Rebuild the original ZIP while preserving repository contents and replace the supplied artifact.
