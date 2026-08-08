package example

import (
	"time"

	"mmo/gameloop"
	"mmo/update"
)

type DemoEntities struct {
	Player  update.EntityID
	Monster update.EntityID
}

// NewMMOLoop wires the hybrid-ECS MMO world into the existing fixed-step game
// loop. Callers may seed players and monsters through world before Run starts.
func NewMMOLoop(
	loopConfig gameloop.Config,
	worldConfig update.Config,
	publisher gameloop.SnapshotPublisher,
	observer gameloop.Observer,
) (*gameloop.Loop, *update.World, error) {
	world, err := update.NewWorld(worldConfig)
	if err != nil {
		return nil, nil, err
	}
	loop, err := gameloop.New(loopConfig, world, publisher, observer)
	if err != nil {
		return nil, nil, err
	}
	return loop, world, nil
}

// SeedDemoScene creates one player and one AI-controlled monster. It is kept
// separate from NewMMOLoop so production callers can load their own scene data.
func SeedDemoScene(world *update.World) (DemoEntities, error) {
	player, err := world.AddPlayer(update.PlayerSpec{
		PlayerID: "player-1",
		Position: update.Vec2{X: 0, Y: 0},
		Health: update.Health{
			Current:           100,
			Maximum:           100,
			RecoveryPerSecond: 1,
		},
		Combat: update.Combat{
			Damage:   12,
			Range:    2,
			Cooldown: 800 * time.Millisecond,
		},
	})
	if err != nil {
		return DemoEntities{}, err
	}
	monster, err := world.AddMonster(update.MonsterSpec{
		Position: update.Vec2{X: 8, Y: 0},
		MaxSpeed: 2.5,
		Health: update.Health{
			Current: 60,
			Maximum: 60,
		},
		Combat: update.Combat{
			Damage:   6,
			Range:    1.5,
			Cooldown: time.Second,
		},
		AI: update.AI{
			AggroRange: 20,
			MoveSpeed:  2.5,
		},
	})
	if err != nil {
		return DemoEntities{}, err
	}
	return DemoEntities{Player: player, Monster: monster}, nil
}

func DemoMoveCommand(playerID string, sequence uint64, velocity update.Vec2) gameloop.Command {
	return gameloop.Command{
		PlayerID:   playerID,
		Seq:        sequence,
		Payload:    update.MoveCommand{Velocity: velocity},
		ReceivedAt: time.Now(),
	}
}

func DemoAttackCommand(
	playerID string,
	sequence uint64,
	applyTick uint64,
	target update.EntityID,
) gameloop.Command {
	return gameloop.Command{
		PlayerID:   playerID,
		Seq:        sequence,
		ApplyTick:  applyTick,
		Payload:    update.AttackCommand{Target: target},
		ReceivedAt: time.Now(),
	}
}

func DemoBurnCommand(
	playerID string,
	sequence uint64,
	target update.EntityID,
) gameloop.Command {
	return gameloop.Command{
		PlayerID: playerID,
		Seq:      sequence,
		Payload: update.ApplyBuffCommand{
			Target: target,
			Buff: update.Buff{
				ID:        "burn",
				Kind:      update.BuffDamageOverTime,
				Duration:  3 * time.Second,
				Period:    time.Second,
				Magnitude: 4,
			},
		},
		ReceivedAt: time.Now(),
	}
}
