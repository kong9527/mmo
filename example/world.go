package example

import (
	"context"
	"fmt"
	"sort"
	"time"

	"mmo/gameloop"
)

type Move struct{ DX, DY int64 }
type Player struct {
	ID      string
	X, Y    int64
	LastSeq uint64
}
type Snapshot struct {
	Tick    uint64
	Players []Player
}

type World struct{ players map[string]*Player }

func NewWorld() *World { return &World{players: make(map[string]*Player)} }

func (w *World) Step(ctx context.Context, tick uint64, dt time.Duration, commands []gameloop.Command) error {
	_ = dt
	// 确定性排序：同一 Tick 内按玩家和序号处理，避免网络到达顺序影响结果。
	sort.SliceStable(commands, func(i, j int) bool {
		if commands[i].PlayerID == commands[j].PlayerID {
			return commands[i].Seq < commands[j].Seq
		}
		return commands[i].PlayerID < commands[j].PlayerID
	})
	for _, cmd := range commands {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		move, ok := cmd.Payload.(Move)
		if !ok {
			return fmt.Errorf("unsupported command payload %T", cmd.Payload)
		}
		p := w.players[cmd.PlayerID]
		if p == nil {
			p = &Player{ID: cmd.PlayerID}
			w.players[cmd.PlayerID] = p
		}
		if cmd.Seq <= p.LastSeq {
			continue
		} // 幂等与乱序保护
		p.X += move.DX
		p.Y += move.DY
		p.LastSeq = cmd.Seq
	}
	return nil
}

func (w *World) Snapshot(context.Context, uint64) (any, error) {
	players := make([]Player, 0, len(w.players))
	for _, p := range w.players {
		players = append(players, *p)
	}
	sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
	return Snapshot{Players: players}, nil
}
