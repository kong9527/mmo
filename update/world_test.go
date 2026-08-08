package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"mmo/gameloop"
)

func mustWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld(DefaultConfig())
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	return w
}

func mustPlayer(t *testing.T, w *World, playerID string, position Vec2) EntityID {
	t.Helper()
	id, err := w.AddPlayer(PlayerSpec{
		PlayerID: playerID,
		Position: position,
		Health:   Health{Current: 100, Maximum: 100},
		Combat:   Combat{Damage: 10, Range: 2, Cooldown: time.Second},
	})
	if err != nil {
		t.Fatalf("AddPlayer() error = %v", err)
	}
	return id
}

func mustMonster(t *testing.T, w *World, position Vec2) EntityID {
	t.Helper()
	id, err := w.AddMonster(MonsterSpec{
		Position: position,
		MaxSpeed: 2,
		Health:   Health{Current: 50, Maximum: 50},
		Combat:   Combat{Damage: 5, Range: 1.5, Cooldown: time.Second},
		AI:       AI{AggroRange: 30, MoveSpeed: 2},
	})
	if err != nil {
		t.Fatalf("AddMonster() error = %v", err)
	}
	return id
}

func TestMoveCommandIsIdempotentAndClamped(t *testing.T) {
	w := mustWorld(t)
	id := mustPlayer(t, w, "p1", Vec2{})
	cmd := gameloop.Command{
		PlayerID: "p1",
		Seq:      1,
		Payload:  MoveCommand{Velocity: Vec2{X: 100}},
	}

	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{cmd, cmd}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	transform, ok := w.Transform(id)
	if !ok {
		t.Fatal("player transform missing")
	}
	if transform.Position.X != w.Config().PlayerMaxSpeed {
		t.Fatalf("x = %v, want %v", transform.Position.X, w.Config().PlayerMaxSpeed)
	}
	state, ok := w.PlayerState(id)
	if !ok || state.LastSeq != 1 {
		t.Fatalf("player state = %#v, present = %v", state, ok)
	}
}

func TestUnsupportedCommandReturnsTypedError(t *testing.T) {
	w := mustWorld(t)
	_ = mustPlayer(t, w, "p1", Vec2{})

	err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{{
		PlayerID: "p1",
		Seq:      1,
		Payload:  struct{}{},
	}})
	if !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Step() error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestAIChoosesNearestPlayerAndSetsChaseVelocity(t *testing.T) {
	w := mustWorld(t)
	near := mustPlayer(t, w, "near", Vec2{X: 5})
	_ = mustPlayer(t, w, "far", Vec2{X: 20})
	monster := mustMonster(t, w, Vec2{})

	if err := w.Step(context.Background(), 1, time.Second, nil); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	ai, ok := w.AI(monster)
	if !ok || ai.Target != near {
		t.Fatalf("AI = %#v, present = %v, want target %d", ai, ok, near)
	}
	movement, ok := w.Movement(monster)
	if !ok || movement.Velocity.X != 2 || movement.Velocity.Y != 0 {
		t.Fatalf("movement = %#v, present = %v", movement, ok)
	}
	transform, _ := w.Transform(monster)
	if transform.Position != (Vec2{}) {
		t.Fatalf("monster moved on acquisition tick: %#v", transform.Position)
	}

	if err := w.Step(context.Background(), 2, time.Second, nil); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	transform, _ = w.Transform(monster)
	if transform.Position.X != 2 {
		t.Fatalf("monster x = %v, want 2", transform.Position.X)
	}
}

func TestAttackRequiresRangeAndCooldown(t *testing.T) {
	w := mustWorld(t)
	_ = mustPlayer(t, w, "attacker", Vec2{})
	target := mustMonster(t, w, Vec2{X: 1})
	attack := gameloop.Command{
		PlayerID: "attacker",
		Seq:      1,
		Payload:  AttackCommand{Target: target},
	}

	if err := w.Step(context.Background(), 1, 50*time.Millisecond, []gameloop.Command{attack}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	health, _ := w.Health(target)
	if health.Current != 40 {
		t.Fatalf("health = %v, want 40", health.Current)
	}

	attack.Seq = 2
	if err := w.Step(context.Background(), 2, 50*time.Millisecond, []gameloop.Command{attack}); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	health, _ = w.Health(target)
	if health.Current != 40 {
		t.Fatalf("health during cooldown = %v, want 40", health.Current)
	}
}

func TestStatBuffsAffectMovementAndAttackForTheirActiveTick(t *testing.T) {
	w := mustWorld(t)
	player := mustPlayer(t, w, "p1", Vec2{})
	target := mustMonster(t, w, Vec2{X: 10})
	commands := []gameloop.Command{
		{
			PlayerID: "p1",
			Seq:      1,
			Payload: ApplyBuffCommand{Target: player, Buff: Buff{
				ID:        "haste",
				Kind:      BuffMoveSpeed,
				Duration:  time.Second,
				Magnitude: 2,
			}},
		},
		{
			PlayerID: "p1",
			Seq:      2,
			Payload: ApplyBuffCommand{Target: player, Buff: Buff{
				ID:        "power",
				Kind:      BuffAttackPower,
				Duration:  time.Second,
				Magnitude: 5,
			}},
		},
		{PlayerID: "p1", Seq: 3, Payload: MoveCommand{Velocity: Vec2{X: 100}}},
		{PlayerID: "p1", Seq: 4, Payload: AttackCommand{Target: target}},
	}

	if err := w.Step(context.Background(), 1, time.Second, commands); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	transform, _ := w.Transform(player)
	if transform.Position.X != 10 {
		t.Fatalf("player x = %v, want 10", transform.Position.X)
	}
	health, _ := w.Health(target)
	if health.Current != 35 {
		t.Fatalf("target health = %v, want 35", health.Current)
	}
}

func TestOutOfRangeAttackDoesNoDamage(t *testing.T) {
	w := mustWorld(t)
	_ = mustPlayer(t, w, "attacker", Vec2{})
	target := mustMonster(t, w, Vec2{X: 100})

	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{{
		PlayerID: "attacker",
		Seq:      1,
		Payload:  AttackCommand{Target: target},
	}}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	health, _ := w.Health(target)
	if health.Current != health.Maximum {
		t.Fatalf("health = %v, want %v", health.Current, health.Maximum)
	}
}

func TestPeriodicDamageKillsAndRemovesEntity(t *testing.T) {
	w := mustWorld(t)
	_ = mustPlayer(t, w, "caster", Vec2{})
	target := mustMonster(t, w, Vec2{})
	if err := w.SetHealth(target, Health{Current: 5, Maximum: 5}); err != nil {
		t.Fatalf("SetHealth() error = %v", err)
	}

	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{{
		PlayerID: "caster",
		Seq:      1,
		Payload: ApplyBuffCommand{Target: target, Buff: Buff{
			ID:        "burn",
			Kind:      BuffDamageOverTime,
			Duration:  time.Second,
			Period:    time.Second,
			Magnitude: 10,
		}},
	}}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if w.Exists(target) {
		t.Fatal("dead target still exists after cleanup")
	}
	if !hasEvent(w.Events(), EventDeath, target) {
		t.Fatal("death event missing")
	}
}

func TestRecoveryClampsAtMaximumHealth(t *testing.T) {
	w := mustWorld(t)
	id := mustPlayer(t, w, "p1", Vec2{})
	if err := w.SetHealth(id, Health{
		Current:           95,
		Maximum:           100,
		RecoveryPerSecond: 10,
	}); err != nil {
		t.Fatalf("SetHealth() error = %v", err)
	}

	if err := w.Step(context.Background(), 1, time.Second, nil); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	health, _ := w.Health(id)
	if health.Current != 100 {
		t.Fatalf("health = %v, want 100", health.Current)
	}
}

func TestSpawnedDuringStepStartsUpdatingNextTick(t *testing.T) {
	w := mustWorld(t)
	_ = mustPlayer(t, w, "target", Vec2{X: 10})
	spec := MonsterSpec{
		Position: Vec2{},
		MaxSpeed: 2,
		Health:   Health{Current: 50, Maximum: 50},
		Combat:   Combat{Damage: 5, Range: 1, Cooldown: time.Second},
		AI:       AI{AggroRange: 30, MoveSpeed: 2},
	}

	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{{
		Payload: SpawnMonsterCommand{Spec: spec},
	}}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	id := w.LastSpawnedEntity()
	before, ok := w.Transform(id)
	if !ok || before.Position != (Vec2{}) {
		t.Fatalf("spawn transform = %#v, present = %v", before, ok)
	}

	if err := w.Step(context.Background(), 2, time.Second, nil); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	after, _ := w.Transform(id)
	if after.Position != (Vec2{}) {
		t.Fatalf("monster moved before AI intent could take effect: %#v", after.Position)
	}

	if err := w.Step(context.Background(), 3, time.Second, nil); err != nil {
		t.Fatalf("third Step() error = %v", err)
	}
	after, _ = w.Transform(id)
	if after.Position.X != 2 {
		t.Fatalf("monster x = %v, want 2", after.Position.X)
	}
}

func TestDespawnCommandRemovesEntityAfterSystems(t *testing.T) {
	w := mustWorld(t)
	target := mustMonster(t, w, Vec2{})

	if err := w.Step(context.Background(), 1, time.Second, []gameloop.Command{{
		Payload: DespawnCommand{Entity: target},
	}}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if w.Exists(target) {
		t.Fatal("despawned entity still exists")
	}
	if hasEvent(w.Events(), EventDeath, target) {
		t.Fatal("administrative despawn emitted a death event")
	}
	if !hasEvent(w.Events(), EventDespawned, target) {
		t.Fatal("despawn event missing")
	}
}

func TestSnapshotIsSortedAndDoesNotAliasWorldState(t *testing.T) {
	w := mustWorld(t)
	first := mustPlayer(t, w, "p1", Vec2{X: 1})
	second := mustMonster(t, w, Vec2{X: 2})
	if first >= second {
		t.Fatalf("test setup IDs = %d, %d", first, second)
	}

	value, err := w.Snapshot(context.Background(), 7)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	snapshot := value.(Snapshot)
	if len(snapshot.Entities) != 2 || snapshot.Entities[0].ID != first || snapshot.Entities[1].ID != second {
		t.Fatalf("snapshot IDs = %#v", snapshot.Entities)
	}
	snapshot.Entities[0].Position.X = 999
	value2, _ := w.Snapshot(context.Background(), 7)
	if value2.(Snapshot).Entities[0].Position.X != 1 {
		t.Fatal("snapshot entity aliases world state")
	}

	snapshot.Events = append(snapshot.Events, Event{Type: EventDamage})
	value3, _ := w.Snapshot(context.Background(), 7)
	if len(value3.(Snapshot).Events) == len(snapshot.Events) {
		t.Fatal("snapshot event slice aliases caller storage")
	}
}

func hasEvent(events []Event, eventType EventType, entity EntityID) bool {
	for _, event := range events {
		if event.Type == eventType && event.Entity == entity {
			return true
		}
	}
	return false
}
