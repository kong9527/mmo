# MMO Update World Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a deterministic hybrid-ECS MMO update world that implements the existing `gameloop.World` interface and demonstrates movement, AI, combat, buffs, recovery, lifecycle events, and snapshots.

**Architecture:** A scene owns an entity registry plus typed sparse-set component stores and is mutated only by its game-loop goroutine. `World.Step` processes ordered commands, executes a fixed system pipeline, and flushes deferred structural changes at the tick boundary.

**Tech Stack:** Go 1.25, standard library, existing `mmo/gameloop` package, `testing` package.

## Global Constraints

- Keep the existing fixed-step `gameloop.Loop` API unchanged.
- Target 500–3000 active entities per scene.
- Tick processing is in-memory only and performs no database, Redis, HTTP, RPC, or broker calls.
- Commands are consumed in the order supplied by `gameloop`.
- New entities first update on the tick after they are spawned.
- Dead entities stop participating immediately and are removed at the tick boundary.
- Snapshots and event slices must not expose mutable world-owned memory.
- Production code is written only after its corresponding test has failed for the expected reason.

---

### Task 1: Typed Sparse-Set Component Storage

**Files:**
- Create: `update/store.go`
- Test: `update/store_test.go`

**Interfaces:**
- Consumes: `EntityID uint64` from `update/types.go` in Task 2; define it temporarily in the test package until Task 2 lands.
- Produces: `newStore[T]() *store[T]`, `set(EntityID, T)`, `get(EntityID) (*T, bool)`, `remove(EntityID) bool`, `has(EntityID) bool`, `len() int`, and stable dense iteration through `entities` and `values`.

- [ ] **Step 1: Write the failing sparse-set test**

```go
func TestStoreRemoveUsesSwapDeleteAndRepairsIndex(t *testing.T) {
	s := newStore[int]()
	s.set(10, 100)
	s.set(20, 200)
	s.set(30, 300)

	if !s.remove(20) {
		t.Fatal("remove returned false")
	}
	if _, ok := s.get(20); ok {
		t.Fatal("removed component still exists")
	}
	got, ok := s.get(30)
	if !ok || *got != 300 {
		t.Fatalf("moved component = %v, %v", got, ok)
	}
}
```

- [ ] **Step 2: Run the test and verify RED**

Run: `go test ./update -run TestStoreRemoveUsesSwapDeleteAndRepairsIndex -count=1`

Expected: FAIL because `newStore` and `store` do not exist.

- [ ] **Step 3: Implement the minimal generic sparse set**

```go
type store[T any] struct {
	entities []EntityID
	values   []T
	index    map[EntityID]int
}

func (s *store[T]) remove(id EntityID) bool {
	i, ok := s.index[id]
	if !ok { return false }
	last := len(s.entities) - 1
	s.entities[i], s.values[i] = s.entities[last], s.values[last]
	s.index[s.entities[i]] = i
	delete(s.index, id)
	var zero T
	s.values[last] = zero
	s.entities = s.entities[:last]
	s.values = s.values[:last]
	return true
}
```

- [ ] **Step 4: Add replacement/lookup tests and verify GREEN**

Run: `go test ./update -run TestStore -count=1`

Expected: PASS for add, replace, lookup, and swap-delete behavior.

- [ ] **Step 5: Commit**

```bash
git add update/store.go update/store_test.go
git commit -m "feat: add typed component store"
```

### Task 2: World Types, Registry, and Deferred Entity Lifecycle

**Files:**
- Create: `update/types.go`
- Create: `update/world.go`
- Test: `update/world_test.go`

**Interfaces:**
- Consumes: `store[T]` from Task 1 and `gameloop.Command`.
- Produces: `Config`, `DefaultConfig()`, `NewWorld(Config)`, `AddPlayer(PlayerSpec)`, `AddMonster(MonsterSpec)`, `Entity(EntityID)`, `Step`, and lifecycle queues.

- [ ] **Step 1: Write failing construction and lifecycle tests**

```go
func TestSpawnedDuringStepStartsUpdatingNextTick(t *testing.T) {
	w := mustWorld(t)
	if err := w.Step(context.Background(), 1, time.Second,
		[]gameloop.Command{{Payload: SpawnMonsterCommand{Spec: testMonster()}}}); err != nil {
		t.Fatal(err)
	}
	id := w.LastSpawnedEntity()
	before, _ := w.Transform(id)
	if err := w.Step(context.Background(), 2, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := w.Transform(id)
	if before.Position == after.Position {
		t.Fatal("monster did not begin updating on following tick")
	}
}
```

- [ ] **Step 2: Run the lifecycle test and verify RED**

Run: `go test ./update -run TestSpawnedDuringStepStartsUpdatingNextTick -count=1`

Expected: FAIL because `World`, specs, and command types do not exist.

- [ ] **Step 3: Define IDs, components, specs, configuration, and registry**

```go
type EntityID uint64
type EntityKind uint8

type Vec2 struct{ X, Y float64 }
type Transform struct { Position Vec2; Facing Vec2 }
type Movement struct { Velocity Vec2; MaxSpeed float64 }
type Health struct { Current, Maximum, RecoveryPerSecond float64; Dead bool }
type Combat struct { Damage, Range float64; Cooldown, Remaining time.Duration }
type AI struct { Target EntityID; AggroRange, MoveSpeed float64 }
type PlayerState struct { PlayerID string; LastSeq uint64 }
```

- [ ] **Step 4: Implement immediate setup and deferred tick-boundary changes**

`AddPlayer` and `AddMonster` insert immediately while `World.stepping` is false.
Command-created entities append to `pendingSpawns`; cleanup appends IDs to
`pendingRemovals`. `flushStructuralChanges` applies each queue once after the
system pipeline.

- [ ] **Step 5: Verify lifecycle tests pass**

Run: `go test ./update -run 'Test(NewWorld|Add|Spawned|Removed)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add update/types.go update/world.go update/world_test.go
git commit -m "feat: add MMO world lifecycle"
```

### Task 3: Ordered Commands and Movement System

**Files:**
- Create: `update/commands.go`
- Create: `update/system_movement.go`
- Modify: `update/world.go`
- Test: `update/commands_test.go`
- Test: `update/system_movement_test.go`

**Interfaces:**
- Consumes: `World`, component stores, and `gameloop.Command`.
- Produces: `MoveCommand`, `AttackCommand`, `ApplyBuffCommand`, `SpawnMonsterCommand`, `DespawnCommand`, `handleCommand`, and `movementSystem.Update`.

- [ ] **Step 1: Write failing movement and sequence tests**

```go
func TestMoveCommandIsIdempotentAndClamped(t *testing.T) {
	w := mustWorld(t)
	id := mustPlayer(t, w, "p1", Vec2{})
	cmd := gameloop.Command{PlayerID: "p1", Seq: 1,
		Payload: MoveCommand{Velocity: Vec2{X: 100}}}
	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{cmd, cmd}); err != nil {
		t.Fatal(err)
	}
	tr, _ := w.Transform(id)
	if tr.Position.X != w.Config().PlayerMaxSpeed {
		t.Fatalf("x = %v", tr.Position.X)
	}
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run: `go test ./update -run 'TestMoveCommand' -count=1`

Expected: FAIL because command dispatch and movement system are missing.

- [ ] **Step 3: Implement command dispatch and sequence checks**

For player commands, look up `PlayerState` by `Command.PlayerID`, ignore
`Seq <= LastSeq`, apply the payload, then store `LastSeq`. Unsupported payloads
return `ErrUnsupportedCommand`; unknown players return `ErrPlayerNotFound`.

- [ ] **Step 4: Implement deterministic movement**

Clamp velocity magnitude to `Movement.MaxSpeed`, then update position by
`velocity * dt.Seconds()`. Skip inactive or dead entities and append a movement
event only when the position changes.

- [ ] **Step 5: Verify movement and command tests pass**

Run: `go test ./update -run 'Test(Move|Command|Sequence)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add update/commands.go update/commands_test.go update/system_movement.go update/system_movement_test.go update/world.go
git commit -m "feat: process movement commands"
```

### Task 4: AI, Combat, Buff, Recovery, and Cleanup Systems

**Files:**
- Create: `update/system_ai.go`
- Create: `update/system_combat.go`
- Create: `update/system_buff.go`
- Create: `update/system_recovery.go`
- Create: `update/system_cleanup.go`
- Test: `update/systems_test.go`

**Interfaces:**
- Consumes: world component stores and queued attack/buff intents.
- Produces: a fixed `system` pipeline with `Name() string` and `Update(context.Context, *World, TickContext) error`.

- [ ] **Step 1: Write failing AI acquisition and chase test**

```go
func TestAIChoosesNearestPlayerAndChases(t *testing.T) {
	w := mustWorld(t)
	near := mustPlayer(t, w, "near", Vec2{X: 5})
	_ = mustPlayer(t, w, "far", Vec2{X: 20})
	monster := mustMonster(t, w, Vec2{})
	if err := w.Step(context.Background(), 1, time.Second, nil); err != nil { t.Fatal(err) }
	ai, _ := w.AI(monster)
	if ai.Target != near { t.Fatalf("target = %d", ai.Target) }
}
```

- [ ] **Step 2: Write failing combat/cooldown test**

```go
func TestAttackRequiresRangeAndCooldown(t *testing.T) {
	w := mustWorld(t)
	attacker := mustPlayer(t, w, "a", Vec2{})
	target := mustMonster(t, w, Vec2{X: 1})
	w.QueueAttack(attacker, target)
	_ = w.Step(context.Background(), 1, 50*time.Millisecond, nil)
	hp, _ := w.Health(target)
	if hp.Current >= hp.Maximum { t.Fatal("attack caused no damage") }
	remaining := hp.Current
	w.QueueAttack(attacker, target)
	_ = w.Step(context.Background(), 2, 50*time.Millisecond, nil)
	hp, _ = w.Health(target)
	if hp.Current != remaining { t.Fatal("cooldown was ignored") }
}
```

- [ ] **Step 3: Run AI/combat tests and verify RED**

Run: `go test ./update -run 'Test(AI|Attack)' -count=1`

Expected: FAIL because the systems do not exist.

- [ ] **Step 4: Implement AI and combat**

AI selects the nearest living player within aggro range, breaking equal-distance
ties by lower `EntityID`, and writes a movement intent toward it. Combat reduces
cooldowns, consumes ordered attack intents, validates liveness/range/cooldown,
applies damage, and emits attack/damage events.

- [ ] **Step 5: Write failing buff/recovery/death tests**

```go
func TestPeriodicDamageCanKillAndCleanupEntity(t *testing.T) {
	w := mustWorld(t)
	target := mustMonster(t, w, Vec2{})
	w.SetHealth(target, Health{Current: 5, Maximum: 5})
	w.ApplyBuff(target, Buff{Kind: BuffDamageOverTime, Duration: time.Second,
		Period: time.Second, Magnitude: 10})
	if err := w.Step(context.Background(), 1, time.Second, nil); err != nil { t.Fatal(err) }
	if w.Exists(target) { t.Fatal("dead entity was not removed") }
}
```

- [ ] **Step 6: Implement buff, recovery, and cleanup**

Buffs keep elapsed/next-trigger durations, apply all elapsed periodic triggers,
expire deterministically, and mark lethal targets dead. Recovery clamps at
maximum HP and skips dead entities. Cleanup emits death/despawn events and
queues physical removal.

- [ ] **Step 7: Verify all system tests pass**

Run: `go test ./update -run 'Test(AI|Attack|Buff|Periodic|Recovery|Dead|Cleanup)' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add update/system_*.go update/systems_test.go update/world.go update/types.go
git commit -m "feat: add MMO simulation systems"
```

### Task 5: Events, Stable Snapshots, and Game-Loop Integration Example

**Files:**
- Create: `update/snapshot.go`
- Create: `update/snapshot_test.go`
- Create: `example/mmo_world.go`
- Create: `example/mmo_world_test.go`
- Modify: `update/world.go`

**Interfaces:**
- Consumes: completed world state and existing `gameloop.New`.
- Produces: `Event`, `Snapshot`, `EntitySnapshot`, copied event output, and `example.NewMMOLoop`.

- [ ] **Step 1: Write failing stable/copying snapshot test**

```go
func TestSnapshotIsSortedAndDoesNotAliasWorldEvents(t *testing.T) {
	w := mustWorld(t)
	second := mustMonster(t, w, Vec2{X: 2})
	first := mustPlayer(t, w, "p", Vec2{X: 1})
	value, err := w.Snapshot(context.Background(), 7)
	if err != nil { t.Fatal(err) }
	s := value.(Snapshot)
	if s.Entities[0].ID != first || s.Entities[1].ID != second {
		t.Fatalf("ids = %#v", s.Entities)
	}
	s.Events = append(s.Events, Event{Type: EventDamage})
	value2, _ := w.Snapshot(context.Background(), 7)
	if len(value2.(Snapshot).Events) == len(s.Events) {
		t.Fatal("snapshot events alias caller storage")
	}
}
```

- [ ] **Step 2: Run snapshot test and verify RED**

Run: `go test ./update -run TestSnapshot -count=1`

Expected: FAIL because snapshot types are missing.

- [ ] **Step 3: Implement stable snapshots and event copies**

Collect active entity IDs, sort ascending, copy component values into
`EntitySnapshot`, and use `append([]Event(nil), w.events...)` for event output.

- [ ] **Step 4: Add compile-time and running-loop integration tests**

```go
var _ gameloop.World = (*update.World)(nil)

func TestNewMMOLoop(t *testing.T) {
	loop, world, err := NewMMOLoop(gameloop.DefaultConfig(), update.DefaultConfig())
	if err != nil || loop == nil || world == nil { t.Fatalf("%v", err) }
}
```

- [ ] **Step 5: Implement `example.NewMMOLoop`**

Construct `update.World`, pass it directly to `gameloop.New`, and return both so
the application can seed the scene before calling `Run`.

- [ ] **Step 6: Verify package tests pass**

Run: `go test ./update ./example ./gameloop -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add update/snapshot.go update/snapshot_test.go update/world.go example/mmo_world.go example/mmo_world_test.go
git commit -m "feat: integrate MMO world with game loop"
```

### Task 6: Final Validation and Handoff

**Files:**
- Modify only files needed to fix validation failures.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: formatted, race-safe, vet-clean project ready for integration.

- [ ] **Step 1: Format all changed Go files**

Run: `gofmt -w update/*.go example/mmo_world.go example/mmo_world_test.go`

- [ ] **Step 2: Run the full test suite**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 3: Run race tests and static analysis**

Run: `go test -race ./... -count=1 && go vet ./...`

Expected: PASS with no warnings.

- [ ] **Step 4: Inspect the final diff**

Run: `git diff --check HEAD~4..HEAD && git status --short && git log --oneline -6`

Expected: no whitespace errors; only planned files are changed.

- [ ] **Step 5: Commit any validation-only corrections**

```bash
git add update example
git commit -m "test: verify MMO update world"
```
