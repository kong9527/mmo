package example

import (
	"mmo/world"
	"testing"

	"mmo/gameloop"
)

func TestNewMMOLoopBuildsCompatibleWorld(t *testing.T) {
	loop, w, err := NewMMOLoop(
		gameloop.DefaultConfig(),
		world.DefaultConfig(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewMMOLoop() error = %v", err)
	}
	if loop == nil || w == nil {
		t.Fatalf("loop = %v, w = %v", loop, w)
	}
}

func TestSeedDemoSceneAndBuildPlayerCommands(t *testing.T) {
	w, err := world.NewWorld(world.DefaultConfig())
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entities, err := SeedDemoScene(w)
	if err != nil {
		t.Fatalf("SeedDemoScene() error = %v", err)
	}
	if !w.Exists(entities.Player) || !w.Exists(entities.Monster) {
		t.Fatalf("seeded entities = %#v", entities)
	}

	move := DemoMoveCommand("player-1", 7, world.Vec2{X: 3})
	if move.PlayerID != "player-1" || move.Seq != 7 {
		t.Fatalf("move command = %#v", move)
	}
	attack := DemoAttackCommand("player-1", 8, 20, entities.Monster)
	if attack.ApplyTick != 20 || attack.Payload.(world.AttackCommand).Target != entities.Monster {
		t.Fatalf("attack command = %#v", attack)
	}
	if attack.ReceivedAt.IsZero() {
		t.Fatal("attack command received time is zero")
	}
}
