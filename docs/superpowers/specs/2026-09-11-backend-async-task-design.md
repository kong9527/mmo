# MMO Backend Async Task Design

## Goal

Add an in-process asynchronous task executor that keeps blocking IO and expensive CPU work outside the World tick goroutine. Accepted tasks produce exactly one Result, and each World drains its own result mailbox at the beginning of a later tick.

## Naming

- `gameloop.Command` remains the ordered input applied by the World.
- `backend.Task` is asynchronous work executed by the Backend worker pool.
- `backend.Result` is immutable output returned to a World mailbox.
- `backend.Executor` owns workers, task IDs, queues, cancellation, and mailbox routing.

## Constraints

- Backend and World run in the same Go process but in different goroutines.
- Backend never receives a World or Entity pointer and never mutates World state.
- World remains the only writer of entity state.
- Submission is non-blocking and returns a typed error when capacity is exhausted.
- An accepted task reserves one mailbox slot, so its terminal Result is not silently lost.
- Results are drained only at the beginning of `World.Step`, with a per-tick limit.
- Existing APIs remain source-compatible where practical; `NewWorld(Config)` remains unchanged.

## Identity

`TaskMeta` contains `WorldID`, `RoomID`, `EntityID`, `EntityEpoch`, `SubmitTick`, and `TaskKind`. World uses `RoomID string` because the existing game-loop manager already addresses rooms by string. `NewWorld` assigns a process-unique WorldID when configuration leaves it at zero, while still allowing explicit IDs. Entity epoch is stored in the existing entity record and exposed by a read method. A result for a missing entity or mismatched epoch is discarded.

## Timeout semantics

Each task supplies `TaskPolicy{Timeout, CancelOnTimeout, TimeoutStatus}`. Cooperative tasks use `CancelOnTimeout=true`. Non-cancelable side-effecting tasks may continue in their worker after the timeout Result is sent; their late completion is discarded. A timed-out or canceled execution keeps its worker slot until `Execute` returns, so tasks that ignore cancellation cannot silently exceed the configured worker count. `ResultOutcomeUnknown` distinguishes an external operation whose final outcome cannot be proven from an ordinary local timeout.

## World integration

World may attach one executor during setup and register one handler per `TaskKind`. At the start of each Step it drains at most `MaxBackendResultsPerTick` results. It verifies that TaskID is pending, then verifies WorldID, RoomID, entity existence, and EntityEpoch before invoking the handler in the World goroutine. The pending TaskID is consumed even when identity is stale, preventing duplicate terminal Results from running a handler twice.

## Shutdown

Unregistering a World closes its mailbox logically and rejects later submissions for that World. Executor Close rejects new submissions, cancels running cooperative tasks, waits for workers to stop, and is idempotent.
