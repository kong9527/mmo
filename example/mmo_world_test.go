package example

import (
	"testing"

	"mmo/gameloop"
	"mmo/update"
)

func TestNewMMOLoopBuildsCompatibleWorld(t *testing.T) {
	loop, world, err := NewMMOLoop(
		gameloop.DefaultConfig(),
		update.DefaultConfig(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewMMOLoop() error = %v", err)
	}
	if loop == nil || world == nil {
		t.Fatalf("loop = %v, world = %v", loop, world)
	}
}

func TestSeedDemoSceneAndBuildPlayerCommands(t *testing.T) {
	world, err := update.NewWorld(update.DefaultConfig())
	if err != nil {
		t.Fatalf("NewWorld() error = %v", err)
	}
	entities, err := SeedDemoScene(world)
	if err != nil {
		t.Fatalf("SeedDemoScene() error = %v", err)
	}
	if !world.Exists(entities.Player) || !world.Exists(entities.Monster) {
		t.Fatalf("seeded entities = %#v", entities)
	}

	move := DemoMoveCommand("player-1", 7, update.Vec2{X: 3})
	if move.PlayerID != "player-1" || move.Seq != 7 {
		t.Fatalf("move command = %#v", move)
	}
	attack := DemoAttackCommand("player-1", 8, 20, entities.Monster)
	if attack.ApplyTick != 20 || attack.Payload.(update.AttackCommand).Target != entities.Monster {
		t.Fatalf("attack command = %#v", attack)
	}
	if attack.ReceivedAt.IsZero() {
		t.Fatal("attack command received time is zero")
	}
}
