package update

import (
	"context"
	"fmt"
	"time"

	"mmo/gameloop"
)

var _ gameloop.World = (*World)(nil)

type entityRecord struct {
	kind   EntityKind
	active bool
}

type spawnRequest struct {
	monster *MonsterSpec
}

type attackIntent struct {
	attacker EntityID
	target   EntityID
}

type TickContext struct {
	Tick  uint64
	Delta time.Duration
}

type system interface {
	Name() string
	Update(context.Context, *World, TickContext) error
}

type World struct {
	cfg Config

	nextEntityID   EntityID
	nextBuffID     uint64
	tick           uint64
	processingTick uint64
	stepping       bool
	lastSpawned    EntityID

	entities map[EntityID]entityRecord
	players  map[string]EntityID

	transforms  *store[Transform]
	movements   *store[Movement]
	health      *store[Health]
	combat      *store[Combat]
	ai          *store[AI]
	playerState *store[PlayerState]
	buffs       *store[[]Buff]

	systems []system
	events  []Event

	attackIntents   []attackIntent
	pendingSpawns   []spawnRequest
	pendingRemovals []EntityID
}

func NewWorld(cfg Config) (*World, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	capacity := cfg.MaxEntities
	w := &World{
		cfg:             cfg,
		entities:        make(map[EntityID]entityRecord, capacity),
		players:         make(map[string]EntityID, capacity),
		transforms:      newStore[Transform](capacity),
		movements:       newStore[Movement](capacity),
		health:          newStore[Health](capacity),
		combat:          newStore[Combat](capacity),
		ai:              newStore[AI](capacity),
		playerState:     newStore[PlayerState](capacity),
		buffs:           newStore[[]Buff](capacity),
		events:          make([]Event, 0, capacity),
		attackIntents:   make([]attackIntent, 0, capacity),
		pendingSpawns:   make([]spawnRequest, 0, 16),
		pendingRemovals: make([]EntityID, 0, 16),
	}
	w.systems = []system{
		movementSystem{},
		aiSystem{},
		combatSystem{},
		buffSystem{},
		recoverySystem{},
		cleanupSystem{},
	}
	return w, nil
}

func (w *World) Config() Config {
	return w.cfg
}

func (w *World) Tick() uint64 {
	return w.tick
}

func (w *World) LastSpawnedEntity() EntityID {
	return w.lastSpawned
}

func (w *World) Exists(id EntityID) bool {
	_, ok := w.entities[id]
	return ok
}

func (w *World) Active(id EntityID) bool {
	record, ok := w.entities[id]
	return ok && record.active
}

func (w *World) Kind(id EntityID) (EntityKind, bool) {
	record, ok := w.entities[id]
	if !ok {
		return EntityUnknown, false
	}
	return record.kind, true
}

func (w *World) Transform(id EntityID) (Transform, bool) {
	value, ok := w.transforms.get(id)
	if !ok {
		return Transform{}, false
	}
	return *value, true
}

func (w *World) Movement(id EntityID) (Movement, bool) {
	value, ok := w.movements.get(id)
	if !ok {
		return Movement{}, false
	}
	return *value, true
}

func (w *World) Health(id EntityID) (Health, bool) {
	value, ok := w.health.get(id)
	if !ok {
		return Health{}, false
	}
	return *value, true
}

func (w *World) Combat(id EntityID) (Combat, bool) {
	value, ok := w.combat.get(id)
	if !ok {
		return Combat{}, false
	}
	return *value, true
}

func (w *World) AI(id EntityID) (AI, bool) {
	value, ok := w.ai.get(id)
	if !ok {
		return AI{}, false
	}
	return *value, true
}

func (w *World) PlayerState(id EntityID) (PlayerState, bool) {
	value, ok := w.playerState.get(id)
	if !ok {
		return PlayerState{}, false
	}
	return *value, true
}

func (w *World) Buffs(id EntityID) ([]Buff, bool) {
	value, ok := w.buffs.get(id)
	if !ok {
		return nil, false
	}
	return append([]Buff(nil), (*value)...), true
}

func (w *World) Events() []Event {
	return append([]Event(nil), w.events...)
}

func (w *World) SetHealth(id EntityID, health Health) error {
	if !w.Exists(id) {
		return fmt.Errorf("%w: %d", ErrEntityNotFound, id)
	}
	if health.Maximum <= 0 {
		return fmt.Errorf("%w: maximum health must be positive", ErrInvalidCommand)
	}
	if health.Current < 0 {
		health.Current = 0
	}
	if health.Current > health.Maximum {
		health.Current = health.Maximum
	}
	health.Dead = health.Current == 0
	w.health.set(id, health)
	return nil
}

func (w *World) AddPlayer(spec PlayerSpec) (EntityID, error) {
	if w.stepping {
		return 0, ErrAlreadyStepping
	}
	return w.addPlayerNow(spec, false)
}

func (w *World) AddMonster(spec MonsterSpec) (EntityID, error) {
	if w.stepping {
		return 0, ErrAlreadyStepping
	}
	return w.addMonsterNow(spec, false)
}

func (w *World) addPlayerNow(spec PlayerSpec, emit bool) (EntityID, error) {
	if spec.PlayerID == "" {
		return 0, fmt.Errorf("%w: player ID is required", ErrInvalidCommand)
	}
	if _, exists := w.players[spec.PlayerID]; exists {
		return 0, fmt.Errorf("%w: %s", ErrPlayerAlreadyAdded, spec.PlayerID)
	}
	id, err := w.allocateEntity(EntityPlayer)
	if err != nil {
		return 0, err
	}

	maxSpeed := spec.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = w.cfg.PlayerMaxSpeed
	}
	health := normalizeHealth(spec.Health, w.cfg.DefaultPlayerHP)
	w.transforms.set(id, Transform{Position: spec.Position})
	w.movements.set(id, Movement{MaxSpeed: maxSpeed})
	w.health.set(id, health)
	w.combat.set(id, normalizeCombat(spec.Combat))
	w.playerState.set(id, PlayerState{PlayerID: spec.PlayerID})
	w.buffs.set(id, nil)
	w.players[spec.PlayerID] = id
	if emit {
		w.emit(Event{Type: EventSpawned, Entity: id, Position: spec.Position})
	}
	return id, nil
}

func (w *World) addMonsterNow(spec MonsterSpec, emit bool) (EntityID, error) {
	id, err := w.allocateEntity(EntityMonster)
	if err != nil {
		return 0, err
	}

	maxSpeed := spec.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = spec.AI.MoveSpeed
	}
	if maxSpeed <= 0 {
		maxSpeed = w.cfg.PlayerMaxSpeed / 2
	}
	ai := spec.AI
	if ai.AggroRange <= 0 {
		ai.AggroRange = w.cfg.DefaultAggroRange
	}
	if ai.MoveSpeed <= 0 {
		ai.MoveSpeed = maxSpeed
	}

	w.transforms.set(id, Transform{Position: spec.Position})
	w.movements.set(id, Movement{MaxSpeed: maxSpeed})
	w.health.set(id, normalizeHealth(spec.Health, w.cfg.DefaultMonsterHP))
	w.combat.set(id, normalizeCombat(spec.Combat))
	w.ai.set(id, ai)
	w.buffs.set(id, nil)
	w.lastSpawned = id
	if emit {
		w.emit(Event{Type: EventSpawned, Entity: id, Position: spec.Position})
	}
	return id, nil
}

func (w *World) allocateEntity(kind EntityKind) (EntityID, error) {
	if len(w.entities) >= w.cfg.MaxEntities {
		return 0, ErrEntityLimit
	}
	w.nextEntityID++
	id := w.nextEntityID
	w.entities[id] = entityRecord{kind: kind, active: true}
	return id, nil
}

func normalizeHealth(value Health, defaultMaximum float64) Health {
	if value.Maximum <= 0 {
		value.Maximum = defaultMaximum
	}
	if value.Current <= 0 && !value.Dead {
		value.Current = value.Maximum
	}
	if value.Current > value.Maximum {
		value.Current = value.Maximum
	}
	if value.Current <= 0 {
		value.Current = 0
		value.Dead = true
	}
	return value
}

func normalizeCombat(value Combat) Combat {
	if value.Damage < 0 {
		value.Damage = 0
	}
	if value.Range < 0 {
		value.Range = 0
	}
	if value.Cooldown < 0 {
		value.Cooldown = 0
	}
	if value.Remaining < 0 {
		value.Remaining = 0
	}
	return value
}

func (w *World) Step(ctx context.Context, tick uint64, dt time.Duration, commands []gameloop.Command) error {
	if tick <= w.tick || dt <= 0 {
		return fmt.Errorf("%w: tick=%d previous=%d delta=%s", ErrInvalidTick, tick, w.tick, dt)
	}
	if w.stepping {
		return ErrAlreadyStepping
	}
	w.stepping = true
	w.processingTick = tick
	defer func() {
		w.processingTick = 0
		w.stepping = false
	}()

	w.events = w.events[:0]
	w.attackIntents = w.attackIntents[:0]

	for i, command := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.handleCommand(command); err != nil {
			return fmt.Errorf("command %d: %w", i, err)
		}
	}

	tickContext := TickContext{Tick: tick, Delta: dt}
	for _, currentSystem := range w.systems {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := currentSystem.Update(ctx, w, tickContext); err != nil {
			return fmt.Errorf("tick %d system %s: %w", tick, currentSystem.Name(), err)
		}
	}
	if err := w.flushStructuralChanges(); err != nil {
		return fmt.Errorf("tick %d structural changes: %w", tick, err)
	}
	w.tick = tick
	return nil
}

func (w *World) queueRemoval(id EntityID) error {
	record, ok := w.entities[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrEntityNotFound, id)
	}
	if !record.active {
		return nil
	}
	record.active = false
	w.entities[id] = record
	w.pendingRemovals = append(w.pendingRemovals, id)
	return nil
}

func (w *World) flushStructuralChanges() error {
	for _, id := range w.pendingRemovals {
		w.removeEntityNow(id)
	}
	w.pendingRemovals = w.pendingRemovals[:0]

	for _, request := range w.pendingSpawns {
		if request.monster != nil {
			if _, err := w.addMonsterNow(*request.monster, true); err != nil {
				return err
			}
		}
	}
	w.pendingSpawns = w.pendingSpawns[:0]
	return nil
}

func (w *World) removeEntityNow(id EntityID) {
	if _, ok := w.entities[id]; !ok {
		return
	}
	if state, exists := w.playerState.get(id); exists {
		delete(w.players, state.PlayerID)
	}
	w.transforms.remove(id)
	w.movements.remove(id)
	w.health.remove(id)
	w.combat.remove(id)
	w.ai.remove(id)
	w.playerState.remove(id)
	w.buffs.remove(id)
	delete(w.entities, id)
	w.emit(Event{Type: EventDespawned, Entity: id})
}

func (w *World) emit(event Event) {
	event.Tick = w.currentTick()
	w.events = append(w.events, event)
}

func (w *World) currentTick() uint64 {
	if w.stepping {
		return w.processingTick
	}
	return w.tick
}

func (w *World) living(id EntityID) bool {
	if !w.Active(id) {
		return false
	}
	health, ok := w.health.get(id)
	return ok && !health.Dead && health.Current > 0
}
