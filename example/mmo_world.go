package example

import (
	"mmo/world"
	"time"

	"mmo/gameloop"
)

type DemoEntities struct {
	Player  world.EntityID
	Monster world.EntityID
}

// NewMMOLoop wires the hybrid-ECS MMO world into the existing fixed-step game
// loop. Callers may seed players and monsters through world before Run starts.
func NewMMOLoop(
	loopConfig gameloop.Config,
	worldConfig world.Config,
	publisher gameloop.SnapshotPublisher,
	observer gameloop.Observer,
) (*gameloop.Loop, *world.World, error) {
	w, err := world.NewWorld(worldConfig)
	if err != nil {
		return nil, nil, err
	}
	loop, err := gameloop.New(loopConfig, w, publisher, observer)
	if err != nil {
		return nil, nil, err
	}
	return loop, w, nil
}

// SeedDemoScene creates one player and one AI-controlled monster. It is kept
// separate from NewMMOLoop so production callers can load their own scene data.
func SeedDemoScene(w *world.World) (DemoEntities, error) {
	player, err := w.AddPlayer(world.PlayerSpec{
		PlayerID: "player-1",
		Position: world.Vec2{X: 0, Y: 0},
		Health: world.Health{
			Current:           100,
			Maximum:           100,
			RecoveryPerSecond: 1,
		},
		Combat: world.Combat{
			Damage:   12,
			Range:    2,
			Cooldown: 800 * time.Millisecond,
		},
	})
	if err != nil {
		return DemoEntities{}, err
	}
	monster, err := w.AddMonster(world.MonsterSpec{
		Position: world.Vec2{X: 8, Y: 0},
		MaxSpeed: 2.5,
		Health: world.Health{
			Current: 60,
			Maximum: 60,
		},
		Combat: world.Combat{
			Damage:   6,
			Range:    1.5,
			Cooldown: time.Second,
		},
		AI: world.AI{
			AggroRange: 20,
			MoveSpeed:  2.5,
		},
	})
	if err != nil {
		return DemoEntities{}, err
	}
	return DemoEntities{Player: player, Monster: monster}, nil
}

func DemoMoveCommand(playerID string, sequence uint64, velocity world.Vec2) gameloop.Command {
	return gameloop.Command{
		PlayerID:   playerID,
		Seq:        sequence,
		Payload:    world.MoveCommand{Velocity: velocity},
		ReceivedAt: time.Now(),
	}
}

func DemoAttackCommand(
	playerID string,
	sequence uint64,
	applyTick uint64,
	target world.EntityID,
) gameloop.Command {
	return gameloop.Command{
		PlayerID:   playerID,
		Seq:        sequence,
		ApplyTick:  applyTick,
		Payload:    world.AttackCommand{Target: target},
		ReceivedAt: time.Now(),
	}
}

func DemoBurnCommand(
	playerID string,
	sequence uint64,
	target world.EntityID,
) gameloop.Command {
	return gameloop.Command{
		PlayerID: playerID,
		Seq:      sequence,
		Payload: world.ApplyBuffCommand{
			Target: target,
			Buff: world.Buff{
				ID:        "burn",
				Kind:      world.BuffDamageOverTime,
				Duration:  3 * time.Second,
				Period:    time.Second,
				Magnitude: 4,
			},
		},
		ReceivedAt: time.Now(),
	}
}
