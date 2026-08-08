# MMO Update World Design

## Goal

Add a production-oriented update layer to the existing `mmo/gameloop` package.
The update layer must implement `gameloop.World`, support 500–3000 active
entities per scene, and provide a complete deterministic MMO example covering
movement, AI, combat, buffs, recovery, entity lifecycle, events, and snapshots.

## Constraints

- Keep the existing fixed-step `gameloop.Loop` API unchanged.
- A scene is single-writer: only the goroutine executing `World.Step` mutates
  simulation state.
- Network goroutines communicate with a scene only through
  `gameloop.Loop.Submit`.
- Tick execution performs in-memory work only. It must not call databases,
  Redis, HTTP, RPC, or message brokers.
- The same initial state and ordered command stream must produce the same state
  and events.
- Entity additions and removals requested while a tick is running are applied
  at the tick boundary so iteration remains stable.

## Chosen Architecture

Use a hybrid ECS design:

- `EntityID` identifies an entity without exposing pointers.
- `Registry` owns entity identity, kind, active state, and player lookup.
- Typed sparse-set component stores keep component values in dense slices for
  cache-friendly iteration and O(1) lookup/removal.
- `World` owns all stores, the command handler, the ordered system pipeline,
  tick events, and deferred structural changes.
- Systems operate on typed component stores in a fixed order. They do not call
  one another directly.

This avoids deep entity inheritance and pointer-heavy component graphs while
remaining simpler than a full archetype ECS.

## Public API

The new `update` package exposes:

- `NewWorld(Config) (*World, error)`
- `(*World).Step(context.Context, uint64, time.Duration, []gameloop.Command) error`
- `(*World).Snapshot(context.Context, uint64) (any, error)`
- Immediate setup helpers `AddPlayer` and `AddMonster`, valid only outside a
  running step and useful for scene construction.
- Command payloads `MoveCommand`, `AttackCommand`, `ApplyBuffCommand`,
  `SpawnMonsterCommand`, and `DespawnCommand`.
- Read-only query helpers needed by integration code and tests.

`World` therefore satisfies the existing `gameloop.World` interface directly.

## Components

- `Transform`: position and facing.
- `Movement`: velocity and maximum movement speed.
- `Health`: current/max HP, recovery rate, and dead flag.
- `Combat`: attack damage, range, cooldown duration, and remaining cooldown.
- `AI`: target, aggro radius, and movement speed.
- `Buffs`: active timed effects with duration, periodic damage, stat modifiers,
  and their next trigger time.
- `PlayerState`: player ID and last accepted command sequence.

Components contain simulation data only. Entity-specific behavior belongs to
systems.

## Tick Pipeline

For every `World.Step` call:

1. Validate the tick and duration and clear the reusable event buffer.
2. Process commands in the order supplied by `gameloop`. Per-player sequence
   checks make commands idempotent and reject stale input.
3. Run systems in this fixed order:
   - movement
   - AI target selection and movement intent
   - combat and cooldowns
   - buffs and periodic effects
   - health recovery
   - cleanup/death handling
4. Apply deferred spawns, component changes, and physical removals.
5. Store the completed tick.

An entity marked dead is skipped immediately by later systems in the same tick,
but its storage is removed only at the tick boundary. An entity spawned during
a tick becomes active after the system pipeline and is first updated on the
next tick.

## Commands and Determinism

`gameloop` already preserves scheduled-command order. The update world will not
re-sort the received slice. This preserves global FIFO semantics for commands
with the same `ApplyTick`.

Commands associated with a player use `Command.Seq` for idempotency. A command
whose sequence is not greater than the player's last accepted sequence is
ignored. Unsupported payloads and references to unknown entities return typed
errors so the loop observer can report the failure.

Movement values are clamped to the configured maximum speed. Floating-point
math is used consistently within the single server process; snapshots sort by
`EntityID` so map order never affects external state.

## Events

Systems append value events to a per-tick event slice. Event types include
entity spawned, movement, attack, damage, buff applied/expired, death, and
despawn. Events preserve append order and are copied into snapshots so callers
cannot mutate world-owned storage.

Events are outputs of the simulation. External publication remains outside the
tick through the existing snapshot publisher.

## Snapshot

`Snapshot` contains the completed tick, sorted entity views, and a copy of the
tick events. Entity views include identity, kind, transform, health, and other
network-relevant state. No component pointer or mutable internal slice escapes.

## Error Handling

- Configuration and setup errors are returned by constructors/helpers.
- Invalid or unsupported commands return descriptive typed errors from `Step`.
- A system error is wrapped with its system name and tick.
- Context cancellation is checked between command processing and systems.
- Panics continue to be contained by the existing `gameloop.safeStep` method.

## Testing

Tests cover:

- sparse-set add, replace, remove, and swap-delete behavior;
- movement and speed clamping;
- player sequence idempotency;
- AI acquisition and chase behavior;
- range/cooldown-gated attacks and damage;
- buff duration and periodic damage;
- recovery and maximum-HP clamping;
- dead entities being skipped and removed safely;
- spawned entities not updating until the following tick;
- stable snapshots and copied event data;
- direct integration with `gameloop.World` and a running loop.

Implementation follows test-first red/green/refactor cycles.
