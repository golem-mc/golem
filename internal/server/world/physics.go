package world

import (
	"cmp"
	"fmt"
	"log/slog"
	"math/rand"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Fluids, gravity and the block updates that follow a block change.
// NotifyChanged, called after every block change the server makes, is the
// neighbour update: immediate reactions (a torch losing its support) happen and
// are returned, rhythmic ones (a fluid spread, a block starting to fall) are
// queued. Tick, once per game tick, advances that queue like vanilla's
// scheduled block ticks and returns every changed block for Block Update
// packets.
//
// Only loaded chunks are touched: SetBlock declines elsewhere and BlockAt reads
// air, so a flood reaching the loaded edge stops without loading anything.
//
// The fluid is vanilla's FlowingFluid: amounts in eighths plus a falling flag,
// sources that wake on a change, spreads across faces both collision shapes
// leave open, new sources where two meet over ground, and obsidian, cobblestone
// and stone where the fluids meet. Deliberately unlike vanilla, a falling
// column is written as level 8 alone rather than deepened by fall height, which
// no client answers differently.
//
// A gravity block over a cell it can fall through is handed to the server as a
// falling block entity and leaves air (or the water a waterlogged block held).
// The falls come back from Tick beside the block changes; the server spawns the
// bodies and hears their landings.
// vanilla: FallingBlock.tick, FallingBlockEntity.fall
//
// Fire runs on the same queue (fire.go), one visit per fire per tick, with the
// TNT its burning primes handed back beside the changes.

// Engine timing in ticks: water moves every 5 and lava every 30 (overworld), a
// block waits 2 before it falls, and each block has at most one visit
// scheduled.
const (
	waterTickDelay = 5
	lavaTickDelay  = 30
	lavaFastDelay  = 10
	fallTickDelay  = 2

	// fallingLevel is the level written for a falling column; vanilla deepens
	// it by fall height.
	fallingLevel = 8

	// maxBlockTicksPerTick and maxFluidTicksPerTick cap the visits one Tick
	// runs of each kind; the rest waits a tick with its due and sequence.
	// vanilla: ServerLevel.MAX_SCHEDULED_TICKS_PER_TICK
	maxBlockTicksPerTick = 65536
	maxFluidTicksPerTick = 65536
)

// updateShapeOrder is the direction order of a shape update: west, east, north,
// south, down, up.
// vanilla: BlockBehaviour.UPDATE_SHAPE_ORDER
var updateShapeOrder = [6][3]int32{
	{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}, {0, 1, 0},
}

// TickPriority ranks, smaller more urgent: among ticks due the same instant the
// higher priority runs first. The two below the block ticks are the piston
// passes: vanilla drains block events after the scheduled-tick pass, and block
// entities (moving pistons landing) after the events, so both wait for every
// block tick of their game tick, the landing after the events.
const (
	tickPriorityExtremelyHigh = -3
	tickPriorityVeryHigh      = -2
	tickPriorityHigh          = -1
	tickPriorityNormal        = 0
	tickPriorityEvent         = 1
	tickPriorityFinalize      = 2
)

// fluidKind numbers the two fluids; the zero value is no fluid, what every
// non-fluid state reads as.
type fluidKind = blocklogic.FluidKind

const (
	fluidNone  = blocklogic.FluidNone
	fluidWater = blocklogic.FluidWater
	fluidLava  = blocklogic.FluidLava
)

// fluidBehaviour holds how water and lava differ: flow delay, amount dropped
// per block, how far the search for a hole reaches, and the gamerule letting
// flows become sources (water's infinite pools).
type fluidBehaviour struct {
	kind              fluidKind
	dropOff           int
	slopeFindDistance int
	conversionRule    GameRuleIndex
}

var fluidBehaviours = [3]fluidBehaviour{
	fluidWater: {kind: fluidWater, dropOff: 1, slopeFindDistance: 4, conversionRule: RuleWaterSourceConversion},
	fluidLava:  {kind: fluidLava, dropOff: 2, slopeFindDistance: 2, conversionRule: RuleLavaSourceConversion},
}

// fluidState is a block read as a fluid: kind, amount in eighths, falling. A
// source is amount 8 not falling, and the zero value is no fluid. A block that
// merely contains water (waterlogged, kelp, a bubble column) reads as a full
// source, as vanilla's fluid states do.
type fluidState struct {
	kind    fluidKind
	amount  int
	falling bool
}

// isSource reports a full cell that is not falling.
func (fs fluidState) isSource() bool {
	return fs.kind != fluidNone && fs.amount >= 8 && !fs.falling
}

// waterByLevel and lavaByLevel hold each fluid's state per legacy level,
// avoiding a registry lookup per change.
var (
	waterByLevel = fluidLevels("minecraft:water")
	lavaByLevel  = fluidLevels("minecraft:lava")

	// The blocks lava and water turn each other into, plus the pair the
	// blue-ice basalt rule needs.
	obsidianState    = registry.BlockStates["minecraft:obsidian"]
	cobblestoneState = registry.BlockStates["minecraft:cobblestone"]
	basaltState      = registry.BlockStates["minecraft:basalt"]
	soulSoilState    = registry.BlockStates["minecraft:soul_soil"]
	blueIceState     = registry.BlockStates["minecraft:blue_ice"]
)

// fluidLevels indexes a fluid's states by their level property.
func fluidLevels(name string) [16]uint16 {
	var levels [16]uint16
	for _, s := range registry.AllStates[name] {
		props := registry.ParseStateProps(s.Props)
		level, err := strconv.Atoi(props["level"])
		if err == nil && level >= 0 && level < len(levels) {
			levels[level] = s.ID
		}
	}
	return levels
}

// fluidStateOf reads the legacy level: 0 a source, 1-7 flowing wedges, 8 a
// falling column; deeper levels, which vanilla writes for tall falls, also read
// as falling.
func fluidStateOf(kind fluidKind, level int) fluidState {
	return fluidState{kind: kind, amount: blocklogic.FluidAmount(level), falling: level >= fallingLevel}
}

// legacyLevel is the block's level property: sources 0, amounts 8-1 as 1-7,
// falling 8 and up.
// vanilla: FluidState.getLegacyLevel
func (fs fluidState) legacyLevel() int {
	if fs.isSource() {
		return 0
	}
	level := max(8-fs.amount, 0)
	if fs.falling {
		level += 8
	}
	return level
}

// createLegacy is the block state for the fluid: air for none, else the fluid's
// block at its level.
func (fs fluidState) createLegacy() uint16 {
	if fs.kind == fluidNone {
		return airState
	}
	return fluidStateAt(fs.kind, fs.legacyLevel())
}

// fluidStateAt resolves a fluid's block state at a legacy level.
func fluidStateAt(kind fluidKind, level int) uint16 {
	if kind == fluidWater {
		return waterByLevel[level]
	}
	return lavaByLevel[level]
}

// fluidHolding reads the fluid a state holds: fluid blocks are themselves,
// waterloggables hold a source when waterlogged, and kelp, seagrass and bubble
// columns hold a source always.
func fluidHolding(state uint16) fluidState {
	f := blocklogic.FluidOf(state)
	if f.Kind == fluidNone {
		return fluidState{}
	}
	return fluidStateOf(f.Kind, int(f.Level))
}

// liquidBlock reports a state BlockStateBase.liquid reads true: water and lava
// at any level, and the bubble column. A waterlogged block holds water but is
// not liquid.
func liquidBlock(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockWater, registry.BlockLava, registry.BlockBubbleColumn:
		return true
	}
	return false
}

// fluidContainer reports a LiquidBlockContainer: the waterloggables and the
// kelp and seagrass family (which hold water but offer none). The bubble column
// is not one, however much water it holds.
func fluidContainer(state uint16) bool {
	return int(state) < len(fluidHoldFlags) && fluidHoldFlags[state]&holdsContainer != 0
}

// canHoldSpecific reports whether the block can take the fluid: containers
// decide (waterloggables only a water source, kelp nothing), and any other
// block says yes, left to the face test and the flood's rules.
// vanilla: FlowingFluid.canHoldSpecificFluid
func canHoldSpecific(state uint16, fs fluidState) bool {
	if int(state) >= len(fluidHoldFlags) {
		return true // non-containers leave rejection to canHoldAny
	}
	flags := fluidHoldFlags[state]
	return flags&holdsContainer == 0 || flags&holdsNoWater == 0 && fs.kind == fluidWater && fs.isSource()
}

var tagWashedAwayByFluids = registry.BlockTagOf("minecraft:washed_away_by_fluids")

const (
	holdsContainer uint8 = 1 << iota
	holdsAny
	holdsNoWater
)

var fluidHoldFlags = buildFluidHoldFlags(registry.CurrentBlockTags())

// Only holdsAny depends on reloadable tags; the container bits stay constant.
var fluidHoldCache atomic.Pointer[fluidHoldTable]

type fluidHoldTable struct {
	tags  *registry.BlockTagLookup
	flags *[registry.StateCount]uint8
}

func buildFluidHoldFlags(tags *registry.BlockTagLookup) *[registry.StateCount]uint8 {
	t := new([registry.StateCount]uint8)
	for id := range t {
		state := uint16(id)
		b := registry.BlockOf(state)
		plant := blocklogic.IsWaterPlant(b)
		if plant && b != registry.BlockBubbleColumn || waterloggable[b] {
			t[id] = holdsContainer | holdsAny
			if plant {
				t[id] |= holdsNoWater
			}
		} else if tags.InTag(b, tagWashedAwayByFluids) {
			t[id] = holdsAny
		}
	}
	return t
}

// currentFluidHoldFlags refreshes the immutable table after a tag reload.
func currentFluidHoldFlags() *[registry.StateCount]uint8 {
	tags := registry.CurrentBlockTags()
	t := fluidHoldCache.Load()
	if t == nil || t.tags != tags {
		t = &fluidHoldTable{tags: tags, flags: buildFluidHoldFlags(tags)}
		fluidHoldCache.Store(t)
	}
	return t.flags
}

// canHoldAny reports whether a state can hold any fluid.
// vanilla: FlowingFluid.canHoldAnyFluid
func canHoldAny(state uint16) bool {
	return int(state) < registry.StateCount && currentFluidHoldFlags()[state]&holdsAny != 0
}

// horizontal4 is north, east, south, west, the order the fluid scans walk.
var horizontal4 = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// gravityBlock indexes the blocks that use fallTick. Scaffolding has its own
// stability tick.
var gravityBlock = func() *[registry.BlockCount]bool {
	t := new([registry.BlockCount]bool)
	for name := range registry.GravityBlocks {
		if b, ok := registry.BlockByName(name); ok {
			t[b] = true
		}
	}
	t[registry.BlockScaffolding] = false
	return t
}()

// concreteForPowder is the powder's concrete twin, indexed by block.
var concreteForPowder = func() (twins [registry.BlockCount]registry.Block) {
	for b := range registry.Block(registry.BlockCount) {
		if c, ok := b.Concrete(); ok {
			twins[b] = c
		}
	}
	return twins
}()

// TrailParticle is a minecraft:trail particle from the block's centre to its
// target, colour 0xRRGGBB, lasting Duration ticks.
type TrailParticle struct {
	X, Y, Z                   float64
	TargetX, TargetY, TargetZ float64
	Color, Duration           int32
}

// The game events a block's own state change posts.
const (
	GameEventBlockOpen  = registry.GameEventBlockOpen
	GameEventBlockClose = registry.GameEventBlockClose

	GameEventBlockActivate   = registry.GameEventBlockActivate
	GameEventBlockDeactivate = registry.GameEventBlockDeactivate
	GameEventBlockAttach     = registry.GameEventBlockAttach
	GameEventBlockDetach     = registry.GameEventBlockDetach
	GameEventNoteBlockPlay   = registry.GameEventNoteBlockPlay
)

// activateGameEvent is the event a lever, button, plate or hook posts as it
// switches on or off.
func activateGameEvent(on bool) registry.GameEvent {
	if on {
		return GameEventBlockActivate
	}
	return GameEventBlockDeactivate
}

// doorGameEvent is the event a door, trapdoor or fence gate posts as it swings.
// vanilla: DoorBlock.setOpen, TrapDoorBlock.playSound
func doorGameEvent(open bool) registry.GameEvent {
	if open {
		return GameEventBlockOpen
	}
	return GameEventBlockClose
}

// ParticleBurst is ServerLevel.sendParticles with an option-less particle:
// Count particles scattered by the spreads.
type ParticleBurst struct {
	ID                        int32
	X, Y, Z                   float64
	SpreadX, SpreadY, SpreadZ float32
	Speed                     float32
	Count                     int32
}

// Effect is a sound or particle event the engine wants played at a change; the
// server sends them as Sound Effect and World Event packets beside the block
// changes.
//
// LevelEventData is a World Event's integer payload where it has one (2001's
// state id, 2003's charge). BreakDrop marks a destruction the engine caused,
// for which the server rolls loot (a shape update taking a door or bed half).
// vanilla: destroyBlock
type Effect struct {
	CustomSound    string         // A custom head's NBT sound name, sent as a direct variable-range holder.
	SoundID        registry.Sound // Zero for effects that do not carry a registered sound.
	LevelEvent     int
	LevelEventData int32
	BreakDrop      uint16
	// RemovedEntity, when nonzero, is the state whose block entity a write at
	// X, Y, Z replaced; the server removes the entity with its side effects.
	RemovedEntity uint16
	// BreakTag is the block entity's compound read before the break, for the
	// loot's skull profile and name; nil when there was none.
	BreakTag      nbt.Compound
	X, Y, Z       int32
	Volume, Pitch float32
	// SoundCategory is the Sound Effect's category; zero is the block category
	// (no block effect plays in master).
	SoundCategory int32
	// ExcludeUser marks a hand-use sound the user's client predicted, which
	// sent back would play twice.
	// vanilla: DoorBlock.playSound, ButtonBlock.playSound, Level.playSound
	ExcludeUser bool
	// BreakColumn is the states below and above the BreakDrop block, for the
	// loot's location checks; nil reads the world when the server rolls.
	BreakColumn *[2]uint16

	// Trail is a trail particle sent beside the sound; nil sends none.
	Trail *TrailParticle

	// HasBlockAction marks a piston move's animation packet (runBlockEvents):
	// BlockAction is 0 extend, 1 retract, 2 abort; BlockActionData the event's
	// facing; BlockActionBlock the piston block drawn.
	HasBlockAction   bool
	BlockAction      uint8
	BlockActionData  uint8
	BlockActionBlock int32

	// Particles is a burst sent with the effect; nil for none.
	Particles *ParticleBurst

	// Vibration is the particle a sensor sends on taking a vibration; nil for
	// none.
	Vibration *VibrationParticle

	// GameEvent is the minecraft:game_event posted at the cell, zero for none.
	// vanilla: Level.gameEvent
	GameEvent registry.GameEvent

	// GameEventAffected is the event's block state, zero for none.
	GameEventAffected uint16

	// GameEventByUser makes the player whose click produced the effect the
	// GameEvent's source entity.
	GameEventByUser bool

	// GameEventSource is the entity standing on a pressure plate, the source
	// of its GameEvent; nil for none.
	GameEventSource *EventSource

	// ListenerEvent delivers a source-preserving event to dynamic listeners
	// after the physics lock is released; block listeners already received it.
	ListenerEvent *Vibration

	// Shriek asks the server to try a shriek; ShriekResponse ends one. Nil
	// otherwise.
	Shriek         *ShriekRequest
	ShriekResponse *ShriekResponse

	// PistonPush is one tick of a moving piston's sweep for the server to apply
	// to bodies; nil otherwise.
	PistonPush *PistonPush
}

// VibrationParticle flies from a vibration's source to the sensor.
// vanilla: VibrationParticleOption
type VibrationParticle struct {
	// X, Y, Z is the source the particle starts at.
	X, Y, Z float64
	// DestX, DestY, DestZ is the sensor's cell, the particle's block position
	// source.
	DestX, DestY, DestZ int32
	// ArrivalTicks is the travel time in ticks.
	ArrivalTicks int
}

// BlockChange is a block physics changed, with its new state; the server sends
// each as a Block Update. PushUp is how far the collision rose over the
// replaced block's, zero if it did not, for Block.pushEntitiesUp. Reshaped
// marks a collision shape that differs from the replaced block's, which
// replans the paths of mobs nearby (vanilla: ServerLevel.sendBlockUpdated);
// only the physics engine's own writes set it.
type BlockChange struct {
	X, Y, Z int32
	State   uint16
	// Reshaped sits beside State so the struct stays 24 bytes.
	Reshaped bool
	// Silent is a shape write inheriting a cleared UPDATE_CLIENTS flag.
	Silent bool
	PushUp float64
}

// physicsChange is the BlockChange of a physics write of state over old.
func physicsChange(x, y, z int32, old, state uint16) BlockChange {
	return BlockChange{X: x, Y: y, Z: z, State: state,
		Reshaped: !slices.Equal(CollisionBoxes(old), CollisionBoxes(state))}
}

// FallingSpawn is a gravity block handed to the server as a falling entity,
// with the state it had less any water. Its cell's change comes beside it so
// the server can send the block update before the spawn. HurtEntities marks a
// landing that hurts what it covers (only the stalactite tip), with the
// per-block amount and cap.
type FallingSpawn struct {
	X, Y, Z int32
	State   uint16
	// HurtEntities, FallHurtAmount and FallHurtMax are setHurtsEntities'
	// values; zero hurts nothing.
	HurtEntities   bool
	FallHurtAmount float64
	FallHurtMax    int
}

// blockKey is a block position as the queue's map key.
type blockKey struct{ x, y, z int32 }

// scheduledTick is a queue entry: the block, the tick it is due, and its
// scheduling order, which breaks ties so visits keep the order their blocks
// changed. event is nonzero for a piston block event (vanilla queues piston
// moves as (position, type, facing) block events drained after the scheduled
// ticks): the low two bits are the type plus one, bits 2-4 the piston's facing
// when queued, which a retraction restores.
type scheduledTick struct {
	pos      blockKey
	due      int64
	priority int
	seq      int64
	event    uint8
	// fluid is the booked fluid type of a fluid visit, zero for any other.
	fluid fluidType
}

// tickQueue orders ticks by due time, then priority (smaller more urgent), then
// scheduling order.
type tickQueue []scheduledTick

func (q tickQueue) less(i, j int) bool {
	if q[i].due != q[j].due {
		return q[i].due < q[j].due
	}
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}
	return q[i].seq < q[j].seq
}

// These are container/heap's algorithms, typed to avoid boxing each tick.
func (q *tickQueue) push(t scheduledTick) {
	*q = append(*q, t)
	q.up(len(*q) - 1)
}

func (q *tickQueue) pop() scheduledTick {
	h := *q
	n := len(h) - 1
	h[0], h[n] = h[n], h[0]
	h.down(0, n)
	t := h[n]
	*q = h[:n]
	return t
}

func (q *tickQueue) remove(i int) scheduledTick {
	h := *q
	n := len(h) - 1
	if n != i {
		h[i], h[n] = h[n], h[i]
		if !h.down(i, n) {
			h.up(i)
		}
	}
	t := h[n]
	*q = h[:n]
	return t
}

func (q tickQueue) init() {
	for i := len(q)/2 - 1; i >= 0; i-- {
		q.down(i, len(q))
	}
}

func (q tickQueue) up(j int) {
	for {
		i := (j - 1) / 2
		if i == j || !q.less(j, i) {
			break
		}
		q[i], q[j] = q[j], q[i]
		j = i
	}
}

func (q tickQueue) down(i0, n int) bool {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow.
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && q.less(j2, j1) {
			j = j2
		}
		if !q.less(j, i) {
			break
		}
		q[i], q[j] = q[j], q[i]
		i = j
	}
	return i > i0
}

// Physics is one world's block-update state: the scheduled-tick queue and the
// game tick it was last advanced to, which visits arriving between ticks are
// scheduled from. The server owns one per world, calls NotifyChanged after its
// own block changes and Tick once per game tick.
type Physics struct {
	// spareChanges and spareEffects are the last Tick's result slices handed
	// back by RecycleTickResults; the next Tick fills them from length 0.
	spareChanges []BlockChange
	spareEffects []Effect
	// pushes backs the Effect.PistonPush pointers of the last Tick; sparePushes
	// is a slab RecycleTickResults took back for the next Tick.
	pushes, sparePushes []PistonPush
	// pushOwner is the first element of the effects batch whose pushes sit in
	// pushes; nil when no outstanding batch owns the slab.
	pushOwner *Effect
	// mu guards everything below: NotifyChanged runs on player connections
	// while Tick runs on the main loop.
	//
	// Lock order: mu is taken before a block entity's lock. UseBlock holds mu
	// through a comparator's read of a dispenser, which takes the entity's
	// lock, so holding an entity lock while asking for mu deadlocks; a dispense
	// visit therefore drops the dispenser's lock before touching the world.
	mu sync.Mutex
	// Shape flags are inherited by nested shape writes under mu.
	skipWireShape, silentShapes bool
	// maxChain caps the neighbour and shape updates of one outermost chain;
	// negative is unlimited. chainCount counts them (skipped ones too),
	// chainDepth is the nesting, chainLogged marks the one error per chain.
	maxChain, chainCount, chainDepth int
	chainLogged                      bool
	w                                *World
	now                              int64
	seq                              int64
	// randomTickKeys is RandomTick's sort buffer, reused between ticks.
	randomTickKeys []uint64
	queue          tickQueue
	pending        map[blockKey]bool
	// fluidBooked holds the fluid visits queued, one per position and type.
	fluidBooked map[fluidBook]struct{}
	finalizeSeq map[blockKey]int64
	// cancelled entries stay in the heap until popped or filtered; their
	// sequence, rather than position, keeps a replacement booking live.
	cancelled map[int64]struct{}
	// tickFilter, when set, names the chunks whose scheduled ticks run this
	// Tick; a due visit elsewhere stays queued. Set between Ticks.
	tickFilter func(ChunkPos) bool
	// parked holds the due visits of chunks tickFilter refused, by chunk.
	parked map[ChunkPos][]scheduledTick

	// touchLeaves defers dripleaf reactions until after redstone, reused per body.
	touchLeaves []blockKey

	// bubbleDue is when each water source's and bubble column's column check is
	// due, a queue beside the fluid visits. Only under mu.
	bubbleDue map[blockKey]int64

	// fallBooked marks stalactites due to fall (hasScheduledTick in
	// SpeleothemBlock.updateShape); set when booked, cleared when the survival
	// visit runs.
	fallBooked map[blockKey]bool

	// survivalBooked marks cells with a survival tick booked, a queue apart
	// from a waterlogged cell's fluid tick so a one-tick survival visit does
	// not wait behind a five-tick water visit. Set when booked, cleared when
	// run.
	survivalBooked map[blockKey]bool

	// ghastBooked marks dried ghasts with a hydration visit booked
	// (DriedGhastBlock.hasScheduledTick), a block tick that must not stand in
	// for, or be stood in for by, a waterlogged cell's fluid visits. Set when
	// booked, cleared when run.
	ghastBooked map[blockKey]bool

	tadpoleHatches  []TadpoleHatch
	frogspawnBooked map[blockKey]bool
	snifferHatches  []SnifferHatch
	// snifferEggBooked marks eggs with their next stage booked.
	snifferEggBooked map[blockKey]bool

	// cauldronBooked marks cauldrons with a stalactite drip tick booked, which
	// rain or snow never books. Set when booked, cleared when run.
	cauldronBooked map[blockKey]bool

	// leafBooked marks leaves with a distance tick booked, apart from a
	// waterlogged leaf's fluid tick so neither delays the other. Set when
	// booked, cleared when run.
	leafBooked map[blockKey]bool
	// scaffoldingBooked keeps stability visits apart from fluid visits.
	scaffoldingBooked map[blockKey]bool

	// redstoneBooked marks big dripleaves and detector rails with a block tick
	// booked, apart from a waterlogged cell's fluid visit; set when booked,
	// cleared when run.
	redstoneBooked map[blockKey]bool

	// coralBooked marks living coral with a die tick booked; set when booked,
	// cleared when run.
	coralBooked map[blockKey]bool

	// sculkBooked marks sculk sensors with a phase change booked, the
	// hasScheduledTick SculkSensorBlock.onPlace reads; set when booked, cleared
	// when run.
	sculkBooked map[blockKey]bool

	// catalysts holds each sculk catalyst's charge cursors, and blooms the
	// catalysts that took a death since the last tick (sculkspread.go). Only
	// under mu.
	catalysts map[blockKey][]chargeCursor
	blooms    []blockKey

	// rodBooked marks struck lightning rods with a release booked; set when
	// booked, cleared when run.
	rodBooked map[blockKey]bool

	// listeners holds sculk sensors with a vibration in flight or an active
	// phase (sculk.go). Only under mu.
	listeners map[blockKey]*vibrationListener

	// pendingEvent is the piston events awaiting the drain (vanilla's
	// blockEvents set); an event already waiting for a block is not queued
	// twice. Only under mu.
	pendingEvent map[blockKey]uint8

	// eventDelay is the head start of a piston event queued now: zero during
	// the scheduled ticks and the event drain (vanilla handles events queued in
	// either in the same pass), one while a ride lands (the block-entity pass
	// after the events), so a finishing ride's event waits a tick.
	eventDelay int64

	// handlingTick is ServerLevel.handlingTick: true during the scheduled ticks
	// and block events (where a zero-tick abort may queue), false during the
	// landing pass and between ticks.
	handlingTick bool

	// torchToggles holds the torches' recent toggles (RECENT_TOGGLES). Only
	// under mu.
	torchToggles map[blockKey][]int64

	// rng is the fire's dice, seeded from the world's seed. Only under mu: the
	// fire tick runs on the main loop, and neighbour-update visits are
	// scheduled without rolling.
	rng *rand.Rand

	// vineRule answers the spread_vines rule; nil reads as on. Only under mu.
	vineRule VineSpreadRule

	// fastLava is the nether's quicker lava (the dimension attribute vanilla
	// reads lava delay and reach from), fixed when the engine is built.
	fastLava bool

	// wireShouldSignal is RedStoneWireBlock.shouldSignal: false only while a
	// wire's own block signal is read, so it cannot read its power back through
	// a conductor it powers. Only under mu.
	wireShouldSignal bool

	// toucher answers what stands on a plate or holds a button down; nil until
	// the server sets it, and the rules read silence.
	toucher Toucher

	// railCarts answers whether a minecart crosses a detector rail's area; nil
	// until the server sets it, and no rail presses.
	railCarts RailCartSource

	// entityOutput answers a comparator's read of an item frame through a
	// conductor; nil until the server sets it, and frames are worth nothing.
	entityOutput EntityAnalogOutput

	// spreader answers whether a fire at a cell may tick (canSpreadFireAround
	// against fire_spread_radius_around_player); nil until the server sets it,
	// and fire ticks everywhere as a rule of -1 reads.
	spreader FireSpreader

	// hooks asks the plugins about writes; nil until the server sets it.
	hooks BlockHooks
	// boneMealActor is the player whose bone meal is running, uuid.Nil for none.
	boneMealActor uuid.UUID

	// copperChestViewers answers whether anyone has the copper chest at a cell
	// open (the ChestBlockEntity opener count's zero test); nil until set, and
	// nobody is ever looking in.
	copperChestViewers CopperChestViewers

	// shulkerLids are the boxes whose lid is not CLOSED, by cell.
	shulkerLids map[[3]int32]*shulkerLid
	// shulkerShoves are this tick's lid steps, taken by the server.
	shulkerShoves []ShulkerShove

	// effects and wakePrimes collect the sounds and TNT the wake, redstone
	// ticks and use path produce. Every entry point that can prime TNT drains
	// them through takeWake under mu, so one caller never clears another's.
	effects    []Effect
	wakePrimes []TNTPrime

	// rides are the cells of the moving pistons still sweeping, in creation
	// order.
	rides   [][3]int32
	rideSet map[[3]int32]struct{}

	// randomEffects collects the random tick pass's effects, drained through
	// TakeRandomEffects.
	randomEffects []Effect

	// dispenses collects the dispenser and dropper visits of the scheduled
	// ticks, drained by Tick's caller; the server owns what happens to the
	// items.
	dispenses []Dispense

	// ghastlings collects the dried ghast hatches, drained through
	// TakeGhastlingSpawns; the server owns the body.
	ghastlings []GhastlingSpawn
	// turtleHatches collects the eggs the random pass hatched, drained through
	// TakeTurtleHatches.
	turtleHatches []TurtleHatch
	// portalTicks collects random ticks on nether portals, drained through
	// TakePortalTicks.
	portalTicks []PortalTick

	// commandRuns collects command block visits, drained like the dispenses;
	// the server runs the command.
	commandRuns []CommandRun

	// structureTriggers collects structure blocks a power rise reached, drained
	// through TakeStructureTriggers; the server does the save or load.
	structureTriggers []StructureTrigger

	// testBlocksTriggered holds the test blocks triggered since their last
	// reset, a flag vanilla does not save.
	// vanilla: TestBlockEntity.triggered
	testBlocksTriggered map[blockKey]bool
}

// Dispense is a dispenser's or dropper's scheduled visit (vanilla's
// dispenseFrom) at the block, facing the direction its facing names in the
// world's numbering. Which slot goes out, and where, is the server's to run.
type Dispense struct {
	X, Y, Z int32
	Dir     int
	// Aim, when set, replaces the face's step as a projectile's direction.
	Aim *[3]float64
}

// TakeDispenses returns and clears the dispense visits.
func (p *Physics) TakeDispenses() []Dispense {
	out := p.dispenses
	p.dispenses = nil
	return out
}

// CommandRun is a command block's scheduled visit with a command due (an
// impulse rise or a repeating turn); the server runs the command.
type CommandRun struct {
	X, Y, Z int32
}

// TakeCommandRuns returns and clears the command block visits.
func (p *Physics) TakeCommandRuns() []CommandRun {
	out := p.commandRuns
	p.commandRuns = nil
	return out
}

// TakeRandomEffects returns and clears the random tick pass's effects.
func (p *Physics) TakeRandomEffects() []Effect {
	out := p.randomEffects
	p.randomEffects = nil
	return out
}

// NewPhysics builds a world's block-update engine, its fire seeded from the
// world's seed so burns replay identically, and its lava at the dimension's
// pace.
func NewPhysics(w *World) *Physics {
	p := &Physics{
		w:                   w,
		maxChain:            DefaultMaxChainedNeighborUpdates,
		pending:             make(map[blockKey]bool),
		fluidBooked:         make(map[fluidBook]struct{}),
		finalizeSeq:         make(map[blockKey]int64),
		cancelled:           make(map[int64]struct{}),
		rideSet:             make(map[[3]int32]struct{}),
		testBlocksTriggered: make(map[blockKey]bool),
		bubbleDue:           make(map[blockKey]int64),
		fallBooked:          make(map[blockKey]bool),
		ghastBooked:         make(map[blockKey]bool),
		frogspawnBooked:     make(map[blockKey]bool),
		snifferEggBooked:    make(map[blockKey]bool),
		cauldronBooked:      make(map[blockKey]bool),
		leafBooked:          make(map[blockKey]bool),
		scaffoldingBooked:   make(map[blockKey]bool),
		coralBooked:         make(map[blockKey]bool),
		redstoneBooked:      make(map[blockKey]bool),
		sculkBooked:         make(map[blockKey]bool),
		catalysts:           make(map[blockKey][]chargeCursor),
		rodBooked:           make(map[blockKey]bool),
		listeners:           make(map[blockKey]*vibrationListener),
		survivalBooked:      make(map[blockKey]bool),
		pendingEvent:        make(map[blockKey]uint8),
		torchToggles:        make(map[blockKey][]int64),
		rng:                 rand.New(rand.NewSource(w.SeedValue())),
		fastLava:            w.FastLava(),
		wireShouldSignal:    true,
	}
	w.mu.Lock()
	w.tickSource = p.chunkTicks
	w.mu.Unlock()
	return p
}

// takeWake returns and clears the wake's effects and TNT. It must run under
// p.mu, which every rule that can queue them already holds.
func (p *Physics) takeWake() ([]Effect, []TNTPrime) {
	effects, primes := p.effects, p.wakePrimes
	p.effects, p.wakePrimes = nil, nil
	return effects, primes
}

// NotifyChanged reports that the block at the position just changed from
// oldState, and returns the changes the wake made immediately. The wake is
// setBlock's flags-3 treatment: the old block's
// affect-neighbours-after-removal, the new block's on-place, then the six
// neighbours' neighbour and shape updates. Fluids, fires and falls react on
// their own schedule through Tick (though a fire placed unsupported goes at
// once). A call where nothing changed is a no-op, as LevelChunk.setBlockState
// declines an unchanged state.
func (p *Physics) NotifyChanged(x, y, z int32, oldState uint16) ([]BlockChange, []Effect, []TNTPrime) {
	return p.notifyChanged(x, y, z, oldState, false)
}

// NotifyPlaced is NotifyChanged for a command's write followed by
// updateNeighboursOnBlockSet: neighborChanged is handed the block now at the
// position, not the old one.
// vanilla: ServerLevel.updateNeighboursOnBlockSet
func (p *Physics) NotifyPlaced(x, y, z int32, oldState uint16) ([]BlockChange, []Effect, []TNTPrime) {
	return p.notifyChanged(x, y, z, oldState, true)
}

// NotifyPlacedHooks is the part of NotifyPlaced a block write owns: the new
// block's on-place and the shape passes, with no neighbour update. A block
// entity's tag loads after it, then NotifyPlacedWake.
// vanilla: Level.setBlock, LevelChunk.setBlockState
func (p *Physics) NotifyPlacedHooks(x, y, z int32, oldState uint16) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	newState := p.w.BlockAt(x, y, z)
	if oldState == newState {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.redstoneOnPlace(x, y, z, oldState, &changes)
	p.shapePass(x, y, z, oldState, newState, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// NotifyPlacedWake is the explicit neighbour update of a command's placement
// after NotifyPlacedHooks and the tag load: the old block's removal reactions
// when the block type changed, then the block and its six neighbours hear of
// the block now at the position, and comparators reading it.
// vanilla: ServerLevel.updateNeighboursOnBlockSet
func (p *Physics) NotifyPlacedWake(x, y, z int32, oldState uint16) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	newState := p.w.BlockAt(x, y, z)
	var changes []BlockChange
	if registry.BlockOf(oldState) != registry.BlockOf(newState) {
		p.redstoneAfterRemoval(x, y, z, oldState, false, true, &changes)
		p.containerRemoved(x, y, z, oldState, newState)
	}
	p.updateAsNeighbor(x, y, z, &changes, true, oldState, newState, [3]int32{})
	p.wakeAround(x, y, z, newState, newState, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

func (p *Physics) notifyChanged(x, y, z int32, oldState uint16, handNew bool) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	newState := p.w.BlockAt(x, y, z)
	if oldState == newState {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.redstoneAfterRemoval(x, y, z, oldState, false, false, &changes)
	p.containerRemoved(x, y, z, oldState, newState)
	p.redstoneOnPlace(x, y, z, oldState, &changes)
	if handNew {
		p.updateAsNeighbor(x, y, z, &changes, true, oldState, newState, [3]int32{})
		p.wakeNeighbors(x, y, z, newState, newState, &changes)
	} else {
		p.neighborUpdate(x, y, z, oldState, newState, &changes)
	}
	p.blockAnalogOutputChanged(x, y, z)
	p.shapePass(x, y, z, oldState, newState, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// shapePass is a write's neighbour shape pass, flanked by the old and new
// states' updateIndirectNeighbourShapes; only redstone wire overrides those,
// for the wires a step above or below its connected sides.
// vanilla: Level.setBlock
func (p *Physics) shapePass(x, y, z int32, oldState, newState uint16, changes *[]BlockChange) {
	p.wireSlopeChanges(x, y, z, oldState, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.wireSlopeChanges(x, y, z, newState, changes)
}

// wakeAround tells the six neighbours of a block set at the position, then
// the comparators reading it.
// vanilla: ServerLevel.updateNeighboursOnBlockSet
func (p *Physics) wakeAround(x, y, z int32, old, changed uint16, changes *[]BlockChange) {
	p.wakeNeighbors(x, y, z, old, changed, changes)
	p.blockAnalogOutputChanged(x, y, z)
}

// NotifyNeighbours is the neighbour update a command's
// updateNeighboursOnBlockSet runs after a non-strict set left the cell
// unchanged: the six neighbours and any reading comparators hear of it.
// NotifyChanged cannot stand in, as it returns early on an unchanged state.
// vanilla: ServerLevel.updateNeighboursOnBlockSet
func (p *Physics) NotifyNeighbours(x, y, z int32, oldState uint16) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	p.wakeAround(x, y, z, oldState, p.w.BlockAt(x, y, z), &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// ShapeUpdateNeighbors runs the shape pass a setBlock without flag 16 owes
// the six neighbours of (x, y, z), for a server write that wakes nothing
// else.
// vanilla: Level.setBlock
func (p *Physics) ShapeUpdateNeighbors(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	p.shapeUpdateNeighbors(x, y, z, &changes)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// SurvivalShapeUpdates is the part of a write's shape pass where a neighbour
// that lost its support goes (a torch, flower, upper door half) with its loot
// in the wake's effects, or books its survival tick.
// vanilla: BlockBehaviour.updateShape, Level.setBlock
func (p *Physics) SurvivalShapeUpdates(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	for _, d := range updateShapeOrder {
		nx, ny, nz := x+d[0], y+d[1], z+d[2]
		state := p.w.BlockAt(nx, ny, nz)
		if railBlocks[registry.BlockOf(state)] {
			// A rail checks support only in neighborChanged, not updateShape.
			continue
		}
		if survivalOf[state].watch != 0 {
			p.supportCheck(nx, ny, nz, state, blocklogic.Opposite(directionOf(d)), &changes)
		} else {
			// The halves of a double plant answer for each other.
			p.doublePlantUpdate(nx, ny, nz, state, false, [3]int32{-d[0], -d[1], -d[2]}, &changes)
		}
	}
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// PlacementShapeUpdates books ticks from a command's shape pass, even for matching writes.
// The command writer has already resolved shapes and restored explicit properties;
// replaying geometry updates here would overwrite those properties (notably wire).
// vanilla: BlockInput.place, Block.updateFromNeighbourShapes
func (p *Physics) PlacementShapeUpdates(x, y, z int32, input uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	block := registry.BlockOf(p.w.BlockAt(x, y, z))
	var changes []BlockChange
	if block == registry.BlockCactus || block == registry.BlockSugarCane {
		p.updateAsNeighbor(x, y, z, &changes, true, input, input, [3]int32{})
		return
	}
	// onPlace may already have reset a powered input; its shape pass books nothing.
	if registry.StatePowered(input) || !isObserver[block] {
		return
	}
	for _, d := range updateShapeOrder {
		p.shapeUpdate(x, y, z, [3]int32{-d[0], -d[1], -d[2]}, &changes)
	}
}

// chunkTicksNow reports whether the chunk holding block column (x, z) passes the
// tick filter. Runs under p.mu.
func (p *Physics) chunkTicksNow(x, z int32) bool {
	return p.tickFilter == nil || p.tickFilter(ChunkPos{X: x >> 4, Z: z >> 4})
}

// park sets a due visit in a chunk that does not tick aside, keeping its due
// time and sequence for when the chunk ticks again; the hot queue never sees
// it again until then. vanilla: LevelTicks keeps ticks per chunk and
// skips chunks failing isPositionTickingWithEntitiesLoaded.
func (p *Physics) park(t scheduledTick) {
	if p.parked == nil {
		p.parked = make(map[ChunkPos][]scheduledTick)
	}
	cp := ChunkPos{X: t.pos.x >> 4, Z: t.pos.z >> 4}
	p.parked[cp] = append(p.parked[cp], t)
}

// releaseParked re-queues the parked visits of every chunk that ticks again.
// Costs one filter call per parked chunk.
func (p *Physics) releaseParked() {
	for cp, ts := range p.parked {
		if p.tickFilter != nil && !p.tickFilter(cp) {
			continue
		}
		for _, t := range ts {
			p.queue.push(t)
		}
		delete(p.parked, cp)
	}
}

// RecycleTickResults hands a Tick's changes and effects back for the next Tick
// to reuse. The caller must not keep either slice, or any PistonPush an effect
// points at, afterwards. The push slab is taken back only with the batch it
// backs, so recycling an older batch leaves a newer Tick's pushes alone.
func (p *Physics) RecycleTickResults(changes []BlockChange, effects []Effect) {
	p.mu.Lock()
	if p.pushOwner != nil && len(effects) > 0 && &effects[0] == p.pushOwner {
		p.sparePushes, p.pushes, p.pushOwner = p.pushes[:0], nil, nil
	}
	clear(effects)
	p.spareChanges, p.spareEffects = changes[:0], effects[:0]
	p.mu.Unlock()
}

// SetTickFilter limits the following Ticks' scheduled visits to the chunks
// ticks accepts; nil lifts the limit.
func (p *Physics) SetTickFilter(ticks func(ChunkPos) bool) {
	p.mu.Lock()
	p.tickFilter = ticks
	p.mu.Unlock()
}

// Tick advances the engine to gameTime, running every visit due and returning
// the changed blocks, the gravity blocks to spawn as falling entities and the
// TNT fires primed. Block and fluid visits have a budget each per tick, the
// overflow running next tick. Separate bookings let a waterlogged block take
// both visits. The results stay valid until they are passed to
// RecycleTickResults; a Tick never reuses a batch that was not recycled.
func (p *Physics) Tick(gameTime int64) ([]BlockChange, []FallingSpawn, []TNTPrime, []Effect) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = gameTime
	// A slab still held here belongs to a batch that was not recycled; leave
	// it to that batch.
	p.pushes, p.pushOwner = nil, nil
	p.adoptLoadedTicks()
	p.handlingTick = true
	changes := p.spareChanges[:0]
	p.spareChanges = nil
	if p.effects == nil {
		p.effects = p.spareEffects[:0]
	}
	p.spareEffects = nil
	var falls []FallingSpawn
	var primes []TNTPrime

	// Container changes since the last tick wake their comparators before the
	// due visits. Each carries the tick it was made in, so the comparator's
	// delay runs from then, not from this drain.
	for _, c := range p.w.takeContainerChanges() {
		p.containerChanged(c.pos[0], c.pos[1], c.pos[2], c.tick)
	}

	// Generated ticks and fluid post-processing visits run on
	// this tick, where the placement's scheduleTick lands. A block that changed
	// since drops its tick.
	// vanilla: SpringFeature.place, SimpleBlockFeature.place, SugarCaneBlock.updateShape
	for _, pos := range p.w.takePendingGenerated() {
		s := p.w.BlockAt(pos[0], pos[1], pos[2])
		if fs := fluidHolding(s); fs.kind != fluidNone && fluidContainer(s) {
			// Post-processing visits the contained fluid, then updateShape
			// books its next visit. vanilla: LevelChunk.postProcessGeneration
			p.fluidTick(pos[0], pos[1], pos[2], fs, &changes)
			p.scheduleFluid(pos[0], pos[1], pos[2])
		} else if fs.kind != fluidNone {
			p.scheduleFluidAt(pos[0], pos[1], pos[2], fs, p.now, tickPriorityNormal)
		} else if isEyeblossom(s) || registry.BlockOf(s) == registry.BlockSugarCane {
			p.schedule(pos[0], pos[1], pos[2], p.now)
		}
	}

	p.bubbleTicks(&changes)
	p.sculkSpreaderTicks(&changes)
	p.shulkerLidTicks(&changes)

	p.releaseParked()
	var deferred []scheduledTick
	for blocks, fluids := 0, 0; (blocks < maxBlockTicksPerTick || fluids < maxFluidTicksPerTick) && len(p.queue) > 0; {
		next := p.queue[0]
		if next.due > p.now {
			break
		}
		p.queue.pop()
		// A spent budget leaves its kind queued while the other keeps draining.
		if blocks >= maxBlockTicksPerTick || fluids >= maxFluidTicksPerTick {
			fluid := next.fluid != 0
			if fluid && fluids >= maxFluidTicksPerTick || !fluid && blocks >= maxBlockTicksPerTick {
				deferred = append(deferred, next)
				continue
			}
		}
		if !p.chunkTicksNow(next.pos.x, next.pos.z) {
			p.park(next)
			continue
		}
		if p.tickCancelled(next) {
			delete(p.cancelled, next.seq)
			continue
		}
		if next.priority == tickPriorityFinalize && next.fluid == 0 {
			delete(p.finalizeSeq, next.pos)
		}
		var fallBooked bool
		switch next.event {
		case frogspawnHatchEvent:
			delete(p.frogspawnBooked, next.pos)
		case snifferEggHatchEvent:
			delete(p.snifferEggBooked, next.pos)
		case driedGhastHydrationEvent:
			// The ghast's own booking, apart from pending: the waterlogged cell
			// also takes fluid visits.
			delete(p.ghastBooked, next.pos)
		case cauldronDripEvent:
			delete(p.cauldronBooked, next.pos)
		case leafTickEvent:
			delete(p.leafBooked, next.pos)
		case scaffoldingTickEvent:
			delete(p.scaffoldingBooked, next.pos)
		case coralTickEvent:
			delete(p.coralBooked, next.pos)
		case redstoneTickEvent:
			delete(p.redstoneBooked, next.pos)
		case sculkTickEvent:
			delete(p.sculkBooked, next.pos)
		case lightningRodTickEvent:
			delete(p.rodBooked, next.pos)
		case survivalTickEvent:
			delete(p.survivalBooked, next.pos)
			fallBooked = p.fallBooked[next.pos]
			delete(p.fallBooked, next.pos)
		default:
			if next.fluid != 0 {
				delete(p.fluidBooked, fluidBook{next.pos, next.fluid})
			} else {
				delete(p.pending, next.pos)
			}
		}

		x, y, z := next.pos.x, next.pos.y, next.pos.z
		state := p.w.BlockAt(x, y, z)
		if int(state) >= registry.StateCount {
			continue
		}
		if next.fluid != 0 {
			// A stale visit spends the budget too.
			// vanilla: ServerLevel.tickFluid runs only the booked type; LevelTicks.collectTicks
			fluids++
			if fs := fluidHolding(state); fs.kind != fluidNone && fluidTypeOf(fs) == next.fluid {
				p.fluidTick(x, y, z, fs, &changes)
			}
			continue
		}
		block := registry.BlockOf(state)
		// The dried ghast's tagged visit runs hydration, not the waterlogged
		// cell's fluid visit.
		if next.event == frogspawnHatchEvent {
			blocks++
			if block == registry.BlockFrogspawn {
				p.frogspawnTick(x, y, z, state, &changes)
			}
			continue
		}
		if next.event == snifferEggHatchEvent {
			blocks++
			if block == registry.BlockSnifferEgg {
				p.snifferEggTick(x, y, z, state, &changes)
			}
			continue
		}
		if next.event == driedGhastHydrationEvent {
			blocks++
			if block == registry.BlockDriedGhast {
				p.driedGhastTick(x, y, z, state, &changes)
			}
			continue
		}
		if next.event == cauldronDripEvent {
			blocks++
			p.cauldronDripTick(x, y, z, state, &changes)
			continue
		}
		if next.event == leafTickEvent {
			blocks++
			p.leafTick(x, y, z, state, &changes)
			continue
		}
		if next.event == scaffoldingTickEvent {
			blocks++
			p.scaffoldingTick(x, y, z, state, &changes, &falls)
			continue
		}
		if next.event == redstoneTickEvent {
			blocks++
			p.redstoneTick(x, y, z, state, &changes)
			continue
		}
		if next.event == coralTickEvent {
			blocks++
			p.coralTick(x, y, z, state, &changes)
			continue
		}
		if next.event == sculkTickEvent {
			blocks++
			p.sculkTick(x, y, z, &changes)
			continue
		}
		if next.event == lightningRodTickEvent {
			blocks++
			if rod := p.w.BlockAt(x, y, z); isLightningRod(rod) {
				p.lightningRodTick(x, y, z, rod, &changes)
			}
			continue
		}
		if next.event == survivalTickEvent {
			if p.survivalTick(x, y, z, state, fallBooked, &changes, &falls) {
				blocks++
			}
			continue
		}
		// A piston's block event, drained after the scheduled-tick pass; the
		// priorities of the event and its ride's landing order the queue that
		// way.
		if next.event != 0 {
			delete(p.pendingEvent, next.pos)
			blocks++
			p.pistonEventTick(x, y, z, state, next.event, &changes)
			continue
		}

		if gravityBlock[block] {
			blocks++
			p.fallTick(x, y, z, state, &changes, &falls)
			continue
		}
		if p.survivalTick(x, y, z, state, false, &changes, &falls) {
			blocks++
			continue
		}
		if block == registry.BlockFire {
			blocks++
			p.fireTick(x, y, z, state, &changes, &primes)
			continue
		}
		// The farm families' one-tick deaths, booked by updateShape when
		// placement or a neighbour change leaves them unable to survive.
		if block == registry.BlockFarmland {
			blocks++
			p.farmlandSurvivalScheduled(x, y, z, &changes)
			continue
		}
		if block == registry.BlockDirtPath {
			blocks++
			p.dirtPathSurvivalScheduled(x, y, z, &changes)
			continue
		}
		if block == registry.BlockSugarCane {
			blocks++
			p.caneSurvivalScheduled(x, y, z, &changes)
			continue
		}
		if block == registry.BlockCactus {
			blocks++
			p.cactusSurvivalScheduled(x, y, z, &changes)
			continue
		}
		if block == registry.BlockFrostedIce {
			blocks++
			p.frostedIceTick(x, y, z, state, &changes)
			continue
		}
		if block == registry.BlockCreakingHeart {
			blocks++
			p.creakingHeartTick(x, y, z, state, &changes)
			continue
		}
		if block == registry.BlockClosedEyeblossom || block == registry.BlockOpenEyeblossom {
			blocks++
			p.eyeblossomScheduledTick(x, y, z, state, &changes)
			continue
		}
		if handled := p.redstoneTick(x, y, z, state, &changes); handled {
			blocks++
		}
	}
	for _, t := range deferred {
		p.queue.push(t)
	}
	p.handlingTick = false
	// The block entities tick after the scheduled ticks.
	// vanilla: ServerLevel.tick
	p.vibrationTicks(&changes)
	p.pistonPushes()
	effects, wakePrimes := p.takeWake()
	if len(p.pushes) == 0 {
		// Nothing points into the slab: keep it for the next sweep.
		if p.sparePushes == nil {
			p.sparePushes = p.pushes[:0]
		}
		p.pushes = nil
	} else {
		p.pushOwner = &effects[0]
	}
	// Redstone ticks queued their TNT in the wake sink; the fire's primes came
	// back through the visit.
	primes = append(primes, wakePrimes...)
	return changes, falls, primes, effects
}

// schedule queues a visit of the block at due unless one is waiting: vanilla
// holds at most one scheduled tick per block, which bounds the queue however
// often the neighbours change.
func (p *Physics) schedule(x, y, z int32, due int64) {
	p.schedulePriority(x, y, z, due, tickPriorityNormal)
}

// scheduleFluid books the visit of the fluid the cell holds. Vanilla keeps
// fluid ticks apart by fluid type and position, so a replaced fluid's visit
// neither holds back nor spends the new fluid's; the type is checked when the
// visit runs.
// vanilla: LiquidBlock.onPlace, ScheduledTick.UNIQUE_TICK_HASH, ServerLevel.tickFluid
func (p *Physics) scheduleFluid(x, y, z int32) {
	if fs := fluidHolding(p.w.BlockAt(x, y, z)); fs.kind != fluidNone {
		p.bookFluid(x, y, z, fs)
	}
}

func (p *Physics) scheduleFluidAt(x, y, z int32, fs fluidState, due int64, priority int) {
	p.bookTick(x, y, z, fluidTypeOf(fs), due, priority)
}

// bookFluid books the visit of fs, the fluid the caller read at the cell.
func (p *Physics) bookFluid(x, y, z int32, fs fluidState) {
	p.scheduleFluidAt(x, y, z, fs, p.now+p.tickDelayOf(fs.kind), tickPriorityNormal)
}

func (p *Physics) schedulePriority(x, y, z int32, due int64, priority int) {
	p.bookTick(x, y, z, 0, due, priority)
}

// bookTick queues a visit unless one is waiting for the same position and
// fluid type (zero for a block tick). Runs under p.mu.
// vanilla: LevelChunkTicks.schedule
func (p *Physics) bookTick(x, y, z int32, typ fluidType, due int64, priority int) {
	// A fluid booking first reserves the saved fluid bookings, so a new one
	// cannot take their place.
	// vanilla: LevelChunkTicks.<init>
	p.adoptTicks(p.w.prepareTick(x, z, true, typ != 0))
	p.insertTick(blockKey{x, y, z}, typ, due, priority)
}

// insertTick queues a visit unless one is waiting for the same key and fluid
// type. Runs under p.mu.
func (p *Physics) insertTick(key blockKey, typ fluidType, due int64, priority int) {
	if typ != 0 {
		book := fluidBook{key, typ}
		if _, ok := p.fluidBooked[book]; ok {
			return
		}
		p.fluidBooked[book] = struct{}{}
	} else {
		if p.pending[key] {
			return
		}
		p.pending[key] = true
	}
	p.seq++
	p.queue.push(scheduledTick{pos: key, due: due, priority: priority, seq: p.seq, fluid: typ})
	if priority == tickPriorityFinalize && typ == 0 {
		p.finalizeSeq[key] = p.seq
	}
}

// BlockTicks is the block ticks pending in a box when taken, each with the
// name of the block its cell held then.
type BlockTicks []boxTick

type boxTick struct {
	tick scheduledTick
	id   string
}

// BlockTicksIn is the block ticks p has pending inside the inclusive box, in
// queue order. Piston events and landings (block events and block-entity ticks
// in vanilla), fluid ticks and the bubble-column checks are left out.
// vanilla: LevelTicks.copyAreaFrom
func (p *Physics) BlockTicksIn(loX, loY, loZ, hiX, hiY, hiZ int32) BlockTicks {
	p.mu.Lock()
	var out BlockTicks
	for t := range p.allTicks {
		if p.tickCancelled(t) || !isTickEvent(t.event) || t.priority == tickPriorityFinalize ||
			t.pos.x < loX || t.pos.x > hiX || t.pos.y < loY || t.pos.y > hiY ||
			t.pos.z < loZ || t.pos.z > hiZ {
			continue
		}
		state := p.w.BlockAt(t.pos.x, t.pos.y, t.pos.z)
		if t.fluid != 0 {
			continue
		}
		if name, _, ok := registry.BlockStateName(state); ok {
			out = append(out, boxTick{tick: t, id: name})
		}
	}
	p.mu.Unlock()
	slices.SortFunc(out, func(a, b boxTick) int {
		return cmp.Or(cmp.Compare(a.tick.due, b.tick.due), cmp.Compare(a.tick.priority, b.tick.priority),
			cmp.Compare(a.tick.seq, b.tick.seq))
	})
	return out
}

// ClearTicksIn drops the block ticks and piston events queued inside the box.
// A moving piston's landing stays: it is the block entity's tick, not a
// scheduled one. Fluid visits go too, where vanilla keeps them; its caller
// has just cleared the box to air, where they would find no fluid.
// vanilla: LevelTicks.clearArea, ServerLevel.clearBlockEvents
func (p *Physics) ClearTicksIn(loX, loY, loZ, hiX, hiY, hiZ int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for cp, ts := range p.parked {
		p.parked[cp] = slices.DeleteFunc(ts, func(t scheduledTick) bool {
			return p.dropTickIn(t, loX, loY, loZ, hiX, hiY, hiZ)
		})
	}
	kept := p.queue[:0]
	for _, t := range p.queue {
		if p.dropTickIn(t, loX, loY, loZ, hiX, hiY, hiZ) {
			continue
		}
		kept = append(kept, t)
	}
	p.queue = kept
	p.queue.init()
}

// dropTickIn reports whether ClearTicksIn removes t, forgetting its booking.
func (p *Physics) dropTickIn(t scheduledTick, loX, loY, loZ, hiX, hiY, hiZ int32) bool {
	if p.tickCancelled(t) {
		delete(p.cancelled, t.seq)
		return true
	}
	if (t.priority != tickPriorityFinalize || t.fluid != 0) &&
		t.pos.x >= loX && t.pos.x <= hiX && t.pos.y >= loY && t.pos.y <= hiY && t.pos.z >= loZ && t.pos.z <= hiZ {
		p.forgetBooking(t)
		return true
	}
	return false
}

// allTicks ranges over the hot queue and then the parked visits, without
// copying either; p.mu must be held.
func (p *Physics) allTicks(yield func(scheduledTick) bool) {
	for _, t := range p.queue {
		if !yield(t) {
			return
		}
	}
	for _, ts := range p.parked {
		for _, t := range ts {
			if !yield(t) {
				return
			}
		}
	}
}

// ScheduleBlockTicks books ticks on p offset by (offX, offY, offZ), keeping
// each one's due tick, priority and order; one whose cell now holds another
// block is dropped, and a cell with a tick pending keeps its own.
// vanilla: LevelTicks.copyAreaFrom
func (p *Physics) ScheduleBlockTicks(ticks BlockTicks, offX, offY, offZ int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range ticks {
		x, y, z := t.tick.pos.x+offX, t.tick.pos.y+offY, t.tick.pos.z+offZ
		p.adoptBlockTick(x, y, z, p.w.BlockAt(x, y, z), t.id, t.tick.due, t.tick.priority)
	}
}

// schedulePistonEvent queues the piston's block event (extend, retract, or a
// zero-tick circuit's abort over the retract; vanilla keeps one event per
// block, replaced by position). It lands p.now + eventDelay out (this pass
// inside the tick, the next when a landing queued it), at a priority after
// every block tick of its game tick.
func (p *Physics) schedulePistonEvent(x, y, z int32, dir int8, typ uint8) {
	_, unloaded := p.w.prepareTick(x, z, false, false)
	p.discardUnloadedTicks(unloaded)
	key := blockKey{x, y, z}
	event := (typ + 1) | uint8(dir)<<2
	if p.pendingEvent[key] == event {
		return
	}
	p.pendingEvent[key] = event
	p.seq++
	p.queue.push(scheduledTick{
		pos: key, due: p.now + p.eventDelay, priority: tickPriorityEvent, seq: p.seq, event: event,
	})
}

// schedulePistonFinalize queues a ride's landing, the block-entity tick the
// game tick after the event that made it, at the block-entity priority after
// every block tick and event. eventDelay being one during the landing is what
// makes a same-tick abort wait for the next game tick.
func (p *Physics) schedulePistonFinalize(x, y, z int32, due int64) {
	p.schedulePriority(x, y, z, due, tickPriorityFinalize)
}

// cancelPistonFinalize drops the ride's queued landing for a forced landing
// that runs first; it would otherwise fire against the block that landing
// wrote.
func (p *Physics) cancelPistonFinalize(x, y, z int32) {
	key := blockKey{x, y, z}
	if seq, ok := p.finalizeSeq[key]; ok {
		delete(p.pending, key)
		delete(p.finalizeSeq, key)
		p.cancelled[seq] = struct{}{}
	}
}

func (p *Physics) tickCancelled(t scheduledTick) bool {
	if t.priority != tickPriorityFinalize || t.fluid != 0 {
		return false
	}
	_, ok := p.cancelled[t.seq]
	return ok
}

// hasTickDueAt reports whether the active queue contains a live visit
// at pos with the requested due tick. Runs under p.mu.
func (p *Physics) hasTickDueAt(pos blockKey, due int64) bool {
	return p.hasTickDueIn(0, pos, due)
}

func (p *Physics) hasTickDueIn(i int, pos blockKey, due int64) bool {
	// A heap child can only be due at the same time as its parent or later.
	if i >= len(p.queue) || p.queue[i].due > due {
		return false
	}
	t := p.queue[i]
	if t.due == due && t.pos == pos && !p.tickCancelled(t) {
		return true
	}
	return p.hasTickDueIn(2*i+1, pos, due) || p.hasTickDueIn(2*i+2, pos, due)
}

// set writes a block, records the change and runs the position's neighbour
// update, so physics' own changes get the server's treatment: water a fall
// opened flows, a torch the flow stranded breaks. A write outside loaded chunks
// is not reported.
func (p *Physics) set(x, y, z int32, state uint16, changes *[]BlockChange) {
	if old, ok := p.setPlaced(x, y, z, state, changes); ok {
		p.wakeNeighbors(x, y, z, old, state, changes)
	}
}

// setPlaced is setBlock's flag 2 for a write whose onPlace matters: the cell's
// own update (updateAsNeighbor placed, standing in for onPlace) runs, but its
// six neighbours hear nothing, so a quasi-powered piston beside it stays put.
// It returns the state before and whether the cell changed.
func (p *Physics) setPlaced(x, y, z int32, state uint16, changes *[]BlockChange) (uint16, bool) {
	old, ok := p.w.SwapBlock(x, y, z, state)
	if !ok {
		return old, false
	}
	*changes = append(*changes, physicsChange(x, y, z, old, state))
	p.updateAsNeighbor(x, y, z, changes, true, old, state, [3]int32{})
	return old, true
}

// setPlacedShaped is setPlaced plus the shape pass. An unchanged cell runs no
// pass.
func (p *Physics) setPlacedShaped(x, y, z int32, state uint16, changes *[]BlockChange) {
	if _, ok := p.setPlaced(x, y, z, state, changes); ok {
		p.shapeUpdateNeighbors(x, y, z, changes)
	}
}

// setShaped is set plus the shape pass a setBlock without flag 16 runs after
// the neighbour update, which is how an observer watching the cell hears it. An
// unchanged cell runs no pass, the gate Level.setBlock's passes sit behind.
func (p *Physics) setShaped(x, y, z int32, state uint16, changes *[]BlockChange) {
	n := len(*changes)
	p.set(x, y, z, state, changes)
	if len(*changes) > n {
		p.shapeUpdateNeighbors(x, y, z, changes)
	}
}

// writeBlock is setBlock's invisible flag 260, used by fire aging: no change
// recorded and no neighbour update, since the client draws the property itself
// and nothing beside an aging fire cares.
func (p *Physics) writeBlock(x, y, z int32, state uint16) {
	p.w.SetBlock(x, y, z, state)
}

// writeBlockShaped is writeBlock plus the shape pass, which only flag 16
// suppresses and 260 lacks: cane and cactus age is an observer's business
// though nobody else's. An unchanged cell runs no pass.
func (p *Physics) writeBlockShaped(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.w.SetBlock(x, y, z, state) {
		p.shapeUpdateNeighbors(x, y, z, changes)
	}
}

// setClient is setBlock's flag 2: recorded for clients but no neighbour update,
// so the caller wakes exactly the neighbours it means to (how wire power walks
// a line in vanilla order instead of fanning out). It returns whether the cell
// changed.
func (p *Physics) setClient(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	old, ok := p.w.SwapBlock(x, y, z, state)
	if !ok {
		return false
	}
	*changes = append(*changes, physicsChange(x, y, z, old, state))
	return true
}

// setClientShaped is setClient plus the shape pass every setBlock without flag
// 16 runs (every flag-2 growth write), which an observer on the cell hears. An
// unchanged cell runs no pass.
func (p *Physics) setClientShaped(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	if !p.setClient(x, y, z, state, changes) {
		return false
	}
	p.shapeUpdateNeighbors(x, y, z, changes)
	return true
}

// setClientMoved is setClient for a piston move's own client-only write.
func (p *Physics) setClientMoved(x, y, z int32, state uint16, changes *[]BlockChange) {
	old, ok := p.w.SwapBlock(x, y, z, state)
	if !ok {
		return
	}
	*changes = append(*changes, physicsChange(x, y, z, old, state))
}

// setMoved is set for a piston move's own write that wakes its neighbours.
func (p *Physics) setMoved(x, y, z int32, state uint16, changes *[]BlockChange) {
	old, ok := p.w.SwapBlock(x, y, z, state)
	if !ok {
		return
	}
	*changes = append(*changes, physicsChange(x, y, z, old, state))
	p.neighborUpdate(x, y, z, old, state, changes)
}

// DefaultMaxChainedNeighborUpdates is max-chained-neighbor-updates' default.
const DefaultMaxChainedNeighborUpdates = 1000000

// SetMaxChainedNeighborUpdates sets the cap on updates in one chain; negative
// is unlimited.
func (p *Physics) SetMaxChainedNeighborUpdates(n int) {
	p.mu.Lock()
	p.maxChain = n
	p.mu.Unlock()
}

// chainStart counts one neighbour or shape update and reports whether it runs.
// Each neighbour counts separately where vanilla counts one entry per
// neighborChanged / updateNeighborsAtExceptFromFacing call, so the cap trips up
// to 6x earlier on a wide fan-out.
// Past the cap the rest of the chain is skipped and the error logged once.
// vanilla: CollectingNeighborUpdater.addAndRun
func (p *Physics) chainStart(x, y, z int32) bool {
	if p.maxChain >= 0 && p.chainCount >= p.maxChain {
		p.chainSkip(x, y, z)
		return false
	}
	p.chainCount++
	p.chainDepth++
	return true
}

func (p *Physics) chainSkip(x, y, z int32) {
	p.chainCount++
	if !p.chainLogged {
		p.chainLogged = true
		slog.Error(fmt.Sprintf("Too many chained neighbor updates. Skipping the rest. First skipped position: %d, %d, %d", x, y, z))
	}
	p.chainReset()
}

// chainHold makes the caller's whole fan-out one chain: siblings share the
// count until chainEnd brings the depth back to 0.
func (p *Physics) chainHold() { p.chainDepth++ }

func (p *Physics) chainEnd() {
	p.chainDepth--
	p.chainReset()
}

// chainReset ends the chain once the outermost update has finished.
func (p *Physics) chainReset() {
	if p.chainDepth == 0 {
		p.chainCount, p.chainLogged = 0, false
	}
}

// neighborUpdate checks the changed position and its six neighbours. Each
// reaction happens now (an unsupported block breaks) or is scheduled (a fluid
// tick, a fall, a fire visit, a leaf distance tick), never both. The position's
// new state is passed along because a leaf's updateShape reads the changed
// block's state, not only that it changed.
func (p *Physics) neighborUpdate(x, y, z int32, old, changed uint16, changes *[]BlockChange) {
	p.chainHold()
	p.updateAsNeighbor(x, y, z, changes, true, old, changed, [3]int32{})
	p.wakeNeighbors(x, y, z, old, changed, changes)
	p.chainEnd()
}

// wakeNeighbors gives the six neighbours of a changed block updateNeighborsAt's
// update: the piece of the full wake the pistons take piecemeal, for writes
// whose flag wakes neighbours and nothing more.
func (p *Physics) wakeNeighbors(x, y, z int32, old, changed uint16, changes *[]BlockChange) {
	p.chainHold()
	listens := p.listens(HookPhysics) // once per fan-out, not per neighbour
	for _, d := range [6][3]int32{
		{0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1},
	} {
		if listens {
			hd := HookDecision{Kind: HookPhysics, X: x + d[0], Y: y + d[1], Z: z + d[2], Old: p.w.BlockAt(x+d[0], y+d[1], z+d[2]), From: [3]int32{x, y, z}}
			if p.ask(&hd) {
				continue
			}
		}
		p.updateAsNeighbor(x+d[0], y+d[1], z+d[2], changes, false, old, changed, [3]int32{-d[0], -d[1], -d[2]})
	}
	p.chainEnd()
}

// BlockReader is the block lookup the shared rules read: *World or a caller's
// smaller interface.
type BlockReader interface {
	BlockAt(x, y, z int32) uint16
}

// ConcretePowderTouchesLiquid is the water test shared by placement,
// updateShape and landing: a neighbour's water counts only where its face
// toward the cell is not sturdy. The cell itself is read only toward DOWN, so a
// dry powder's cell reads nothing below it.
// vanilla: ConcretePowderBlock.touchesLiquid
func ConcretePowderTouchesLiquid(w BlockReader, x, y, z int32) bool {
	below := w.BlockAt(x, y-1, z)
	if HoldsWater(w.BlockAt(x, y, z)) && HoldsWater(below) &&
		!registry.FaceSturdy(below, dirUp, registry.SupportFull) {
		return true
	}
	for _, d := range [5]int{dirUp, dirNorth, dirSouth, dirWest, dirEast} {
		delta := dirDelta[d]
		state := w.BlockAt(x+delta[0], y+delta[1], z+delta[2])
		if HoldsWater(state) && !registry.FaceSturdy(state, blocklogic.Opposite(d), registry.SupportFull) {
			return true
		}
	}
	return false
}

// updateAsNeighbor gives one position its neighbour update: a fluid schedules
// its move, a gravity block over air its fall, a fire rebuilds its state, an
// unsupported block breaks (through set, like any change). placed means the
// position itself changed rather than a neighbour, the difference between a
// placed fire (survival checked, state kept) and one whose neighbour changed
// (state rebuilt). changed is the state updateShape's argument carries, at the
// position (placed) or beside it; old is the state before, the block
// neighborChanged passes. from is the offset to the changed block,
// updateShape's direction (snowy dirt and farmland ask for a change from
// above), zero when the position itself changed.
func (p *Physics) updateAsNeighbor(x, y, z int32, changes *[]BlockChange, placed bool, old, changed uint16, from [3]int32) {
	p.updateAsNeighborParts(x, y, z, changes, placed, old, changed, from, true)
}

func (p *Physics) updateAsNeighborParts(x, y, z int32, changes *[]BlockChange, placed bool, old, changed uint16, from [3]int32, neighbors bool) {
	if !p.chainStart(x, y, z) {
		return
	}
	p.updateAsNeighborBody(x, y, z, changes, placed, old, changed, from, neighbors)
	p.chainEnd()
}

func (p *Physics) updateAsNeighborBody(x, y, z int32, changes *[]BlockChange, placed bool, old, changed uint16, from [3]int32, neighbors bool) {
	state := p.w.BlockAt(x, y, z)
	p.bubbleWaterChanged(x, y, z, state)
	fs := fluidHolding(state)
	if fs.kind != fluidNone && !fluidContainer(state) {
		if fs.kind == fluidLava && !p.shouldSpreadLiquid(x, y, z, changes) {
			return // the lava met water: obsidian, cobblestone or basalt at once
		}
		p.bookFluid(x, y, z, fs)
		return
	}
	// A block's own placement leaves it standing (BlockInput.place keeps a
	// state updateShape answers air for), but families with a survival tick
	// book it, as updateFromNeighbourShapes does.
	if s := survivalOf[state]; s.watch != 0 && (!placed || s.tick != 0) {
		toward := anyDirection
		if from != ([3]int32{}) {
			toward = directionOf(from)
		}
		if p.supportCheck(x, y, z, state, toward, changes) {
			return
		}
	}
	block := registry.BlockOf(state)
	// Tall seagrass holds water but inherits DoublePlantBlock.updateShape,
	// which books no fluid tick.
	if fs.kind != fluidNone && block != registry.BlockTallSeagrass {
		// For seagrass and sea pickles, BlockInput.place keeps the state
		// after a failed support check, but the shape offer books no water.
		// vanilla: SeagrassBlock.updateShape, SeaPickleBlock.updateShape
		if !placed || (block != registry.BlockSeagrass && block != registry.BlockSeaPickle) ||
			canSurvive(p.w, x, y, z, state) {
			p.bookFluid(x, y, z, fs)
		}
	}
	if placed && block == registry.BlockFrogspawn && registry.BlockOf(old) != block {
		p.scheduleFrogspawn(x, y, z)
	}
	if placed && block == registry.BlockSnifferEgg && registry.BlockOf(old) != block {
		p.scheduleSnifferEgg(x, y, z)
	}
	if p.doublePlantUpdate(x, y, z, state, placed, from, changes) {
		return
	}
	if (placed || neighbors) && p.spongeChanged(x, y, z, block, placed, old, changes) {
		return
	}
	if block == registry.BlockFire {
		if placed {
			// An unsupported fire goes at once; either way the block keeps a
			// visit coming.
			// vanilla: BaseFireBlock.onPlace
			if !p.fireCanSurvive(x, y, z) {
				p.setShaped(x, y, z, airState, changes)
			}
			p.scheduleFire(x, y, z)
			return
		}
		// Rebuild the state the neighbours make at the fire's age, or air where
		// unsupported. The visit is already promised; this schedule only
		// catches ones not yet promised.
		// vanilla: FireBlock.updateShape
		if !p.fireCanSurvive(x, y, z) {
			p.setShaped(x, y, z, airState, changes)
			return
		}
		if rebuilt := p.fireStateFor(x, y, z, fireAgeOf(state)); rebuilt != state {
			p.setShaped(x, y, z, rebuilt, changes)
		}
		p.scheduleFire(x, y, z)
		return
	}
	if block == registry.BlockSoulFire {
		// Soul fire stands only on a soul-fire base and ticks nothing once it
		// stands.
		if !blocklogic.SoulFireBase(p.w.BlockAt(x, y-1, z)) {
			p.setShaped(x, y, z, airState, changes)
		}
		return
	}
	if hugeMushroomBlocks[block] {
		// A zero offset is a piston landing the block, not a neighbour.
		if !placed && from != ([3]int32{}) {
			if next := hugeMushroomShapeUpdate(state, directionOf(from), changed); next != state {
				p.setClientShaped(x, y, z, next, changes)
			}
		}
		return
	}
	if block.IsLeaves() {
		if placed {
			// LeavesBlock.onPlace does nothing: a placement's distance was
			// computed with its state, and an engine tick's change carries the
			// distance it computed.
			return
		}
		// A neighbour change books a distance tick one game tick out, unless
		// the leaf is already at distance 1 beside a block that holds leaves,
		// where the tick would recompute the same. A waterlogged leaf's fluid
		// visit is booked in the container branch above, in its own slot.
		// vanilla: LeavesBlock.updateShape
		if i := leafDistanceOf(changed) + 1; i != 1 || leafDistanceOf(state) != 1 {
			p.scheduleLeaf(x, y, z, p.now+leafTickDelay)
		}
		return
	}
	if isLivingCoral(block) {
		p.scheduleCoralDie(x, y, z, state)
		return
	}
	if block == registry.BlockBigDripleaf && !placed && from == [3]int32{0, 1, 0} &&
		registry.BlockOf(changed) == registry.BlockBigDripleaf {
		// vanilla: BigDripleafBlock.updateShape — a leaf gaining a leaf above
		// becomes a stem keeping facing and waterlogging; other changes above
		// fall through to the power check.
		p.setShaped(x, y, z, bigDripleafShapeUpdate(state, dirUp, changed), changes)
		return
	}
	if block == registry.BlockScaffolding {
		// vanilla: ScaffoldingBlock.onPlace, ScaffoldingBlock.updateShape
		p.scheduleScaffolding(x, y, z, p.now+scaffoldingTickDelay)
		return
	}
	if gravityBlock[block] {
		// Powder water has reached becomes concrete instead of falling,
		// updateShape's half of the rule.
		// vanilla: ConcretePowderBlock.updateShape
		if concrete := concreteForPowder[block]; concrete != registry.BlockAir && ConcretePowderTouchesLiquid(p.w, x, y, z) {
			if made, ok := p.veto(HookForm, x, y, z, concrete.DefaultState(), [3]int32{}); ok {
				p.setShaped(x, y, z, made, changes)
			}
			return
		}
		if p.fallsInto(x, y, z) {
			p.schedule(x, y, z, p.now+fallTickDelay)
		}
		return
	}
	// The farm families' own updateShape rules run ahead of the generic support
	// check, which is coarser than what they survive on: cane and cactus book a
	// one-tick break with loot, farmland books its turn to dirt, spreading dirt
	// rewrites snowy from the change above.
	if block == registry.BlockSugarCane {
		if !caneCanSurvive(p.w, x, y, z) {
			p.schedule(x, y, z, p.now+caneTickDelay)
		}
		return
	}
	if block == registry.BlockCactus {
		if !cactusCanSurvive(p.w, x, y, z) {
			p.schedule(x, y, z, p.now+cactusTickDelay)
		}
		return
	}
	if block == registry.BlockAttachedMelonStem || block == registry.BlockAttachedPumpkinStem {
		if !placed && from != ([3]int32{}) {
			p.attachedStemUpdateShape(x, y, z, state, from, changed, changes)
		}
		return
	}
	// A placement's updateFromNeighbourShapes includes the face above.
	// vanilla: Block.updateFromNeighbourShapes, FarmlandBlock.updateShape, PathBlock.updateShape
	if block == registry.BlockFarmland && (placed || from == [3]int32{0, 1, 0}) {
		if !farmlandCanSurvive(p.w, x, y, z) {
			p.schedule(x, y, z, p.now+farmlandTickDelay)
		}
		return
	}
	if block == registry.BlockDirtPath && (placed || from == [3]int32{0, 1, 0}) {
		if !dirtPathCanSurvive(p.w, x, y, z) {
			p.schedule(x, y, z, p.now+dirtPathTickDelay)
		}
		return
	}
	if block == registry.BlockFrostedIce {
		if placed {
			// vanilla: FrostedIceBlock.onPlace
			p.schedule(x, y, z, p.now+frostedIcePlaceDelay(p.rng))
			return
		}
		// vanilla: FrostedIceBlock.neighborChanged — a frosted ice neighbour
		// changed and fewer than two frosted neighbours remain, so it melts at
		// once.
		if neighbors && registry.BlockOf(old) == registry.BlockFrostedIce && p.frostedIceFewerNeighboursThan(x, y, z, frostedIceNeighboursToMelt) {
			p.meltIce(x, y, z, state, changes, &p.effects)
		}
		return
	}
	if snowyDirtTable[block] && !placed {
		if from != ([3]int32{}) {
			if next := snowyDirtShapeUpdate(state, directionOf(from), changed); next != state {
				p.setShaped(x, y, z, next, changes)
			}
		}
		return
	}
	if railBlocks[block] && (placed || neighbors) {
		p.railBlockUpdate(x, y, z, state, placed, old, changes)
		return
	}
	if neighbors {
		p.redstoneNeighborChanged(x, y, z, state, changes, placed)
	}
}

// shapeUpdateNeighbors is a flags-3 setBlock's shape pass after the neighbour
// update: each neighbour, in UPDATE_SHAPE_ORDER, takes the state it would have
// now.
// vanilla: BlockBehaviour.updateNeighbourShapes
func (p *Physics) shapeUpdateNeighbors(x, y, z int32, changes *[]BlockChange) {
	p.chainHold()
	defer p.chainEnd()
	if p.skipWireShape || p.silentShapes {
		for _, d := range updateShapeOrder {
			p.flaggedShapeUpdate(x+d[0], y+d[1], z+d[2], d, changes)
		}
		return
	}
	for _, d := range updateShapeOrder {
		p.shapeUpdate(x+d[0], y+d[1], z+d[2], d, changes)
	}
}

// fallsInto reports whether the block falls through the one below; below the
// dimension's floor reads as void air, so a block on the bottom row falls out.
// vanilla: FallingBlock.isFree, FallingBlock.tick
func (p *Physics) fallsInto(x, y, z int32) bool {
	switch minY := p.w.MinY(); {
	case y < minY:
		return false
	case y == minY:
		return true
	}
	return registry.CanFallThrough(p.w.BlockAt(x, y-1, z))
}

// fallTick is a gravity block's visit: when the block below is free it becomes
// a falling entity, its cell left as its own fluid made solid (water if it held
// some, else air), the spawn returned beside the change so the server sends the
// update before the body. A visit finding it held up again is spent.
// vanilla: FallingBlock.tick
func (p *Physics) fallTick(x, y, z int32, state uint16, changes *[]BlockChange, falls *[]FallingSpawn) {
	if !p.fallsInto(x, y, z) {
		return
	}
	p.fallSpawn(x, y, z, state, changes, falls)
}

// fallSpawn is a fall's tail, shared with scaffolding's stability tick: the
// cell becomes its own fluid made solid and the body carries the state without
// its water.
// vanilla: FallingBlockEntity.fall
func (p *Physics) fallSpawn(x, y, z int32, state uint16, changes *[]BlockChange, falls *[]FallingSpawn) {
	carried := state
	if dry, ok := registry.WithBool(state, registry.PropWaterlogged, false); ok && dry != state {
		// fall()'s setValue(WATERLOGGED, false), other properties kept, and the
		// fluid's own legacy block.
		carried = dry
		p.setShaped(x, y, z, waterByLevel[0], changes)
	} else {
		p.setShaped(x, y, z, airState, changes)
	}
	*falls = append(*falls, FallingSpawn{X: x, Y: y, Z: z, State: carried})
}

// behaviourOf is a fluid's spread parameters; the nether's fast lava drops off
// by 1 and looks 4 blocks for a slope.
// vanilla: LavaFluid.getDropOff, getSlopeFindDistance
func (p *Physics) behaviourOf(kind fluidKind) fluidBehaviour {
	b := fluidBehaviours[kind]
	if kind == fluidLava && p.fastLava {
		b.dropOff = 1
		b.slopeFindDistance = 4
	}
	return b
}

// tickDelayOf is a fluid's visit delay: water 5, lava 30, or 10 where the
// dimension's lava runs fast.
func (p *Physics) tickDelayOf(kind fluidKind) int64 {
	if kind == fluidLava && p.fastLava {
		return lavaFastDelay
	}
	if kind == fluidLava {
		return lavaTickDelay
	}
	return waterTickDelay
}

// fluidHeight is a fluid cell's depth in blocks: 1 with the same fluid above,
// else amount/9.
// vanilla: FluidState.getHeight
func (fs fluidState) fluidHeight(w BlockReader, x, y, z int32) float64 {
	if fluidHolding(w.BlockAt(x, y+1, z)).kind == fs.kind {
		return 1
	}
	return float64(fs.amount) / 9
}

// faceOf is the face a horizontal step crosses, as axis and sense.
func faceOf(dx, dz int32) (int, bool) {
	switch {
	case dx > 0:
		return 0, true
	case dx < 0:
		return 0, false
	case dz > 0:
		return 2, true
	default:
		return 2, false
	}
}

// passWall reports whether the face between two states (first to second) lets
// fluid through: a full cube either side is a wall, two empty shapes are open,
// otherwise Shapes.mergedFaceOccludes decides.
// vanilla: FlowingFluid.canPassThroughWall
func passWall(srcState uint16, axis int, positive bool, dstState uint16) bool {
	dst := collisionOf(dstState)
	if dst.fullCube {
		return false
	}
	src := collisionOf(srcState)
	if src.fullCube {
		return false
	}
	if len(src.boxes) == 0 && len(dst.boxes) == 0 {
		return true
	}
	return !cachedFaceOccludes(srcState, dstState, axis, positive)
}

// canMaybePassThrough is the coarse gate every fluid path runs: the cell ahead
// is not a source of this fluid, can hold fluid, and shows a face both
// collision shapes leave open.
// vanilla: FlowingFluid.canMaybePassThrough
func canMaybePassThrough(kind fluidKind, srcState uint16, axis int, positive bool, dstState uint16, dstFS fluidState) bool {
	if dstFS.kind == kind && dstFS.isSource() {
		return false
	}
	if !canHoldAny(dstState) {
		return false
	}
	return passWall(srcState, axis, positive, dstState)
}

// canReplaceWith reports whether the incoming fluid displaces the held one: the
// empty fluid yields to anything, water only to another fluid falling on it,
// lava to water reaching 4/9 deep or more, which is why a thin lava flow holds
// a flood out and a deep one goes under.
// vanilla: FluidState.canBeReplacedWith
func canReplaceWith(w BlockReader, held fluidState, x, y, z int32, incoming fluidKind, down bool) bool {
	switch held.kind {
	case fluidNone:
		return true
	case fluidWater:
		return down && incoming != fluidWater
	case fluidLava:
		return held.fluidHeight(w, x, y, z) >= 0.44444445 && incoming == fluidWater
	}
	return false
}

// newLiquid is what the fluid at the position should be now: two sources over
// body-holding ground make a source, the same fluid above makes a falling top,
// otherwise the strongest neighbour (across an open face) less the drop-off, or
// empty.
// vanilla: FlowingFluid.getNewLiquid
func (p *Physics) newLiquid(x, y, z int32, kind fluidKind) fluidState {
	return newLiquidAt(p.w, x, y, z, p.w.BlockAt(x, y, z), p.behaviourOf(kind))
}

type fluidSpreadReader interface {
	BlockReader
	BoolRule(GameRuleIndex) bool
}

func newLiquidAt(w fluidSpreadReader, x, y, z int32, state uint16, b fluidBehaviour) fluidState {
	kind := b.kind
	sources, highest := 0, 0
	for _, d := range horizontal4 {
		nx, nz := x+d[0], z+d[1]
		ns := w.BlockAt(nx, y, nz)
		nfs := fluidHolding(ns)
		if nfs.kind != kind {
			continue
		}
		axis, positive := faceOf(d[0], d[1])
		if !passWall(state, axis, positive, ns) {
			continue
		}
		if nfs.isSource() {
			sources++
		}
		if nfs.amount > highest {
			highest = nfs.amount
		}
	}
	if sources >= 2 && w.BoolRule(b.conversionRule) && supportsFluidSource(w.BlockAt(x, y-1, z), kind) {
		return fluidState{kind: kind, amount: 8}
	}
	if y < worldMinY+worldHeight {
		aboveState := w.BlockAt(x, y+1, z)
		if afs := fluidHolding(aboveState); afs.kind == kind && passWall(state, 1, true, aboveState) {
			return fluidState{kind: kind, amount: 8, falling: true}
		}
	}
	amount := highest - b.dropOff
	if amount <= 0 {
		return fluidState{}
	}
	return fluidState{kind: kind, amount: amount}
}

func supportsFluidSource(state uint16, kind fluidKind) bool {
	if collisionSolid(state) {
		return true
	}
	f := blocklogic.FluidOf(state)
	return f.Kind == kind && f.IsSource()
}

// The downward face is already open in both directions. With matching fluid
// above and no support for source conversion, the neighbors cannot affect the
// level.
func downwardLiquidAt(w fluidSpreadReader, x, y, z int32, originState, destinationState uint16, b fluidBehaviour) fluidState {
	if blocklogic.FluidOf(originState).Kind == b.kind && !supportsFluidSource(w.BlockAt(x, y-1, z), b.kind) {
		return fluidState{kind: b.kind, amount: 8, falling: true}
	}
	return newLiquidAt(w, x, y, z, destinationState, b)
}

// spread pours down first, where the face below is open and the cell will take
// it, as the fluid that cell would become (so a waterfall stays full however
// far it falls). A flow that cannot pour and is not over a hole holds still; a
// source always floods. The flood goes to the nearest holes the slope search
// finds, every tie, or all four ways when none is in reach.
func (p *Physics) spread(x, y, z int32, fs fluidState, changes *[]BlockChange) {
	if fs.kind == fluidNone {
		return
	}
	state := p.w.BlockAt(x, y, z)
	bx, by, bz := x, y-1, z
	belowState := p.w.BlockAt(bx, by, bz) // air past the world's floor
	if canMaybePassThrough(fs.kind, state, 1, false, belowState, fluidHolding(belowState)) {
		newBelow := downwardLiquidAt(p.w, bx, by, bz, state, belowState, p.behaviourOf(fs.kind))
		if canReplaceWith(p.w, fluidHolding(belowState), bx, by, bz, newBelow.kind, true) &&
			canHoldSpecific(belowState, newBelow) {
			p.spreadTo(bx, by, bz, belowState, newBelow, [3]int32{x, y, z}, changes)
			if pourSurroundedBySources(p.w, x, y, z, fs.kind) {
				// Hemmed in by sources on three sides, the pour also floods.
				p.spreadToSides(x, y, z, state, fs, changes)
			}
			return
		}
	}
	if !fs.isSource() && isWaterHole(state, belowState, fluidState{kind: fs.kind, amount: 8, falling: true}, nil) {
		return // a flow hanging over a hole pours instead of flooding
	}
	p.spreadToSides(x, y, z, state, fs, changes)
}

// isWaterHole reports whether the face down from a cell opens onto the same
// fluid or something the flow can fill. A nil holdFlags consults the live tag
// table after the wall and same-fluid checks.
// vanilla: FlowingFluid.isWaterHole
func isWaterHole(topState, belowState uint16, flowing fluidState, holdFlags *[registry.StateCount]uint8) bool {
	if !passWall(topState, 1, false, belowState) {
		return false
	}
	if fluidHolding(belowState).kind == flowing.kind {
		return true
	}
	if holdFlags == nil {
		holdFlags = currentFluidHoldFlags()
	}
	return int(belowState) < len(holdFlags) && holdFlags[belowState]&holdsAny != 0 && canHoldSpecific(belowState, flowing)
}

// spreadReach is the cache's reach from the origin: the search starts one out,
// goes slopeFindDistance further, and reads one more cell at the end.
const spreadReach = 6

// spreadSide is the edge of the square of cached cells.
const spreadSide = 2*spreadReach + 1

// spreadContext caches the plane of states a spread walks, since the slope
// search rereads cells. Cells are keyed relative to the origin; outside the
// square they are read uncached.
// vanilla: FlowingFluid.SpreadContext
type spreadContext struct {
	w     fluidSpreadReader
	world *World // live worlds can cache the chunk sections
	// A spread uses one tag snapshot throughout its slope search.
	holdFlags     *[registry.StateCount]uint8
	topWords      [4]*blockWords
	belowWords    [4]*blockWords
	topFill       [4]uint16
	belowFill     [4]uint16
	cx0, cz0      int32
	topYOff       int
	belowYOff     int
	y             int32
	ox, oz        int32
	kind          fluidKind
	flowingType   fluidState
	lastState     uint16
	lastHoleTop   uint16
	lastHoleBelow uint16
	lastFlags     uint8
	lastHoleFlags uint8
	chunksLoaded  uint8
	states        [spreadSide * spreadSide]uint16
	flags         [spreadSide * spreadSide]uint8
	failedDepth   [spreadSide * spreadSide][4]uint8
}

// spreadOffsets is each cell's (dx, dz) from the square's corner.
var spreadOffsets = func() (t [spreadSide * spreadSide][2]int8) {
	for i := range t {
		t[i] = [2]int8{int8(i % spreadSide), int8(i / spreadSide)}
	}
	return
}()

func (c *spreadContext) init(w fluidSpreadReader, x, y, z int32, kind fluidKind) {
	c.w = w
	c.world, _ = w.(*World)
	c.y = y
	c.ox = x
	c.oz = z
	c.cx0 = (x - spreadReach) >> 4
	c.cz0 = (z - spreadReach) >> 4
	c.topYOff = int(uint32(y-worldMinY)&15) << 8
	c.belowYOff = int(uint32(y-1-worldMinY)&15) << 8
	c.kind = kind
	c.flowingType = fluidState{kind: kind, amount: 8, falling: true}
	c.holdFlags = currentFluidHoldFlags()
}

func newSpreadContext(w fluidSpreadReader, x, y, z int32, kind fluidKind) spreadContext {
	var c spreadContext
	c.init(w, x, y, z, kind)
	return c
}

// loadChunk resolves one chunk in the 2x2 quadrant and snapshots its block
// storage at y and y-1.
func (c *spreadContext) loadChunk(q uint, cx, cz int32) {
	ch := c.world.sharedChunkAt(ChunkPos{X: cx, Z: cz})
	if ch == nil {
		// A chunk worker can publish this neighbour during the search.
		return
	}
	c.chunksLoaded |= 1 << q
	c.topFill[q] = airState
	c.belowFill[q] = airState
	if ly := uint32(c.y - worldMinY); ly < worldHeight {
		s := &ch.sections[ly>>4]
		c.topWords[q] = s.blocks.Load()
		c.topFill[q] = s.fill
	}
	if ly := uint32(c.y - 1 - worldMinY); ly < worldHeight {
		s := &ch.sections[ly>>4]
		c.belowWords[q] = s.blocks.Load()
		c.belowFill[q] = s.fill
	}
}

// index maps a position to its cell, or reports it outside the square.
func (c *spreadContext) index(x, z int32) (int, bool) {
	dx, dz := x-c.ox+spreadReach, z-c.oz+spreadReach
	if uint32(dx) >= spreadSide || uint32(dz) >= spreadSide {
		return 0, false
	}
	return int(dz)*spreadSide + int(dx), true
}

const (
	cellEnterable uint8 = 1 << iota
	cellEmptyShape
	cellFullShape
	cellKnown
	cellHoleKnown
	cellHole
)

// cellFlags classifies the state for this spread's fluid and tag snapshot.
func (c *spreadContext) cellFlags(state uint16) uint8 {
	var flags uint8
	fs := fluidHolding(state)
	if (fs.kind != c.kind || !fs.isSource()) && int(state) < len(c.holdFlags) && c.holdFlags[state]&holdsAny != 0 && canHoldSpecific(state, c.flowingType) {
		flags |= cellEnterable
	}
	shape := collisionOf(state)
	if len(shape.boxes) == 0 {
		flags |= cellEmptyShape
	}
	if shape.fullCube {
		flags |= cellFullShape
	}
	return flags
}

// load reads a cell's state and classification into the cache.
func (c *spreadContext) load(i int) {
	off := spreadOffsets[i]
	x := c.ox + int32(off[0]) - spreadReach
	z := c.oz + int32(off[1]) - spreadReach
	var state uint16
	if c.world != nil {
		cx, cz := x>>4, z>>4
		q := uint((cx-c.cx0)|(cz-c.cz0)<<1) & 3
		if c.chunksLoaded&(1<<q) == 0 {
			c.loadChunk(q, cx, cz)
		}
		if b := c.topWords[q]; b != nil {
			state = b.load(c.topYOff | int(z&15)<<4 | int(x&15))
		} else if c.chunksLoaded&(1<<q) != 0 {
			state = c.topFill[q]
		} else {
			state = c.w.BlockAt(x, c.y, z)
		}
	} else {
		state = c.w.BlockAt(x, c.y, z)
	}
	c.states[i] = state
	if c.lastFlags&cellKnown != 0 && state == c.lastState {
		c.flags[i] = c.lastFlags
		return
	}
	flags := c.cellFlags(state) | cellKnown
	c.lastState = state
	c.lastFlags = flags
	c.flags[i] = flags
}

// cell reads a cached state and its slope-search classification.
func (c *spreadContext) cell(x, z int32) (state uint16, flags uint8) {
	i, ok := c.index(x, z)
	if !ok {
		state = c.w.BlockAt(x, c.y, z)
		return state, c.cellFlags(state)
	}
	if c.flags[i]&cellKnown == 0 {
		c.load(i)
	}
	return c.states[i], c.flags[i] & (cellEnterable | cellEmptyShape | cellFullShape)
}

// state reads a cached block state of the plane.
func (c *spreadContext) state(x, z int32) uint16 {
	state, _ := c.cell(x, z)
	return state
}

// isHole reports, cached, whether a cell hangs over ground the fluid would pour
// through.
func (c *spreadContext) isHole(x, z int32) bool {
	i, ok := c.index(x, z)
	if !ok {
		return isWaterHole(c.state(x, z), c.w.BlockAt(x, c.y-1, z), c.flowingType, c.holdFlags)
	}
	if c.flags[i]&cellKnown == 0 {
		c.load(i)
	}
	return c.holeAt(i)
}

// holeAt caches whether an already loaded cell hangs over a hole.
func (c *spreadContext) holeAt(i int) bool {
	if c.flags[i]&cellHoleKnown == 0 {
		off := spreadOffsets[i]
		x := c.ox + int32(off[0]) - spreadReach
		z := c.oz + int32(off[1]) - spreadReach
		top := c.states[i]
		var below uint16
		if c.world != nil {
			q := uint((x>>4-c.cx0)|(z>>4-c.cz0)<<1) & 3
			if b := c.belowWords[q]; b != nil {
				below = b.load(c.belowYOff | int(z&15)<<4 | int(x&15))
			} else if c.chunksLoaded&(1<<q) != 0 {
				below = c.belowFill[q]
			} else {
				below = c.w.BlockAt(x, c.y-1, z)
			}
		} else {
			below = c.w.BlockAt(x, c.y-1, z)
		}
		var holeFlags uint8
		if c.lastHoleFlags&cellHoleKnown != 0 && top == c.lastHoleTop && below == c.lastHoleBelow {
			holeFlags = c.lastHoleFlags
		} else {
			holeFlags = cellHoleKnown
			if isWaterHole(top, below, c.flowingType, c.holdFlags) {
				holeFlags |= cellHole
			}
			c.lastHoleTop = top
			c.lastHoleBelow = below
			c.lastHoleFlags = holeFlags
		}
		c.flags[i] |= holeFlags
	}
	return c.flags[i]&cellHole != 0
}

func (p *Physics) spreadToSides(x, y, z int32, state uint16, fs fluidState, changes *[]BlockChange) {
	flows, winners := fluidSideFlows(p.w, x, y, z, state, fs, p.behaviourOf(fs.kind))
	for i, d := range horizontal4 {
		if !winners[i] {
			continue
		}
		// An earlier write or plugin hook may have changed the target.
		tx, tz := x+d[0], z+d[1]
		p.spreadTo(tx, y, tz, p.w.BlockAt(tx, y, tz), flows[i], [3]int32{x, y, z}, changes)
	}
}

// fluidSideFlows plans a flood if the fluid has strength left (a full cell less
// its drop-off; a falling column 7): to every direction tied for the nearest
// hole, or all where none is in reach.
// vanilla: FlowingFluid.spreadToSides, getSpread
func fluidSideFlows(w fluidSpreadReader, x, y, z int32, state uint16, fs fluidState, b fluidBehaviour) (flows [4]fluidState, winners [4]bool) {
	gate := fs.amount - b.dropOff
	if fs.falling {
		gate = 7
	}
	if gate <= 0 {
		return flows, winners
	}
	var ctx spreadContext
	ctx.init(w, x, y, z, fs.kind)
	c := &ctx
	var sides [4]struct {
		into, held     fluidState
		state          uint16
		eligible, hole bool
	}
	anyDiffers := false
	eligible := 0
	for i, d := range horizontal4 {
		tx, tz := x+d[0], z+d[1]
		tState := c.state(tx, tz)
		tFS := fluidHolding(tState)
		axis, positive := faceOf(d[0], d[1])
		if !canMaybePassThrough(fs.kind, state, axis, positive, tState, tFS) {
			continue
		}
		side := &sides[i]
		if fluidContainer(tState) {
			side.into = newLiquidAt(w, tx, y, tz, tState, b)
			if !canHoldSpecific(tState, side.into) {
				continue
			}
		}
		side.state = tState
		side.held = tFS
		side.eligible = true
		eligible++
		anyDiffers = anyDiffers || tFS.kind != fs.kind
	}
	// Every eligible side already holds this fluid, which no write replaces.
	if !anyDiffers {
		return flows, winners
	}
	hasHole := false
	if eligible > 1 {
		// Already-wet sides can still outrank a side that can receive a write.
		for i, d := range horizontal4 {
			side := &sides[i]
			if !side.eligible {
				continue
			}
			side.hole = c.isHole(x+d[0], z+d[1])
			hasHole = hasHole || side.hole
		}
	}
	lowest := 1000
	for i, d := range horizontal4 {
		side := &sides[i]
		if !side.eligible || (hasHole && !side.hole) {
			continue
		}
		distance := 0
		// A lone outlet wins at any distance.
		if !hasHole && eligible > 1 {
			ti, _ := c.index(x+d[0], z+d[1])
			distance = c.slopeDistance(ti, 1, i^2, min(b.slopeFindDistance, lowest))
		}
		if distance < lowest {
			winners = [4]bool{}
		}
		if distance > lowest {
			continue
		}
		// An eligible hole sets the distance even if its fluid cannot be displaced.
		lowest = distance
		winners[i] = true
	}
	// Finish planning every write before a spread or plugin hook changes the world.
	for i, d := range horizontal4 {
		if !winners[i] {
			continue
		}
		side := &sides[i]
		if side.held.kind == fs.kind {
			winners[i] = false
			continue
		}
		tx, tz := x+d[0], z+d[1]
		if !fluidContainer(side.state) {
			side.into = newLiquidAt(w, tx, y, tz, side.state, b)
		}
		winners[i] = canReplaceWith(w, side.held, tx, y, tz, side.into.kind, false)
		flows[i] = side.into
	}
	return flows, winners
}

// spreadStep follows horizontal4's north, east, south, west order.
var spreadStep = [4]int{-spreadSide, 1, spreadSide, -1}

// slopeDistance finds the nearest hole without stepping back. i is a loaded
// cell in the cached square, which the walk cannot leave; from is the
// horizontal4 direction it came in by. reach also bounds the distance past
// which the exact result no longer matters; exhausting it returns 1000.
// vanilla: FlowingFluid.getSlopeDistance
func (c *spreadContext) slopeDistance(i, pass, from, reach int) int {
	// Even at the bound, the search checks the adjacent cells for holes.
	depth := uint8(1 + max(reach-pass, 0))
	failedDepth := &c.failedDepth[i][from]
	if *failedDepth >= depth {
		return 1000
	}
	lowest := 1000
	flags := c.flags[i]
	for d, step := range spreadStep {
		if d == from {
			continue
		}
		n := i + step
		nf := c.flags[n]
		if nf&cellKnown == 0 {
			c.load(n)
			nf = c.flags[n]
		}
		if nf&cellEnterable == 0 || (flags|nf)&cellFullShape != 0 {
			continue
		}
		if flags&nf&cellEmptyShape == 0 {
			axis, positive := faceOf(horizontal4[d][0], horizontal4[d][1])
			if cachedFaceOccludes(c.states[i], c.states[n], axis, positive) {
				continue
			}
		}
		if nf&cellHoleKnown == 0 {
			c.holeAt(n)
			nf = c.flags[n]
		}
		if nf&cellHole != 0 {
			return pass
		}
		if pass >= reach {
			continue
		}
		if v := c.slopeDistance(n, pass+1, d^2, reach); v < lowest {
			lowest = v
			reach = min(reach, v-1)
		}
	}
	if lowest == 1000 {
		*failedDepth = depth
	}
	return lowest
}

// vanilla: FlowingFluid.sourceNeighborCount
func pourSurroundedBySources(w BlockReader, x, y, z int32, kind fluidKind) bool {
	sources, misses := 0, 0
	for _, d := range horizontal4 {
		f := blocklogic.FluidOf(w.BlockAt(x+d[0], y, z+d[1]))
		if f.Kind == kind && f.IsSource() {
			sources++
			if sources == 3 {
				return true
			}
		} else {
			misses++
			if misses == 2 {
				return false
			}
		}
	}
	return false
}

// spreadTo writes the flow into a cell. A container takes a water source
// through placeLiquid, waterlogging and booking its own visit; anything else is
// replaced, water dropping its loot and lava only fizzing; lava pouring onto
// water makes stone, with the fizz either way and nothing written where a block
// held the water. Every write runs the flags-3 shape pass, so an observer hears
// the flow arrive.
// vanilla: FlowingFluid.spreadTo, LavaFluid.spreadTo, Level.setBlock
func (p *Physics) spreadTo(x, y, z int32, state uint16, target fluidState, from [3]int32, changes *[]BlockChange) {
	if p.listens(HookFromTo) {
		d := HookDecision{Kind: HookFromTo, X: x, Y: y, Z: z, Old: state, From: from}
		if p.ask(&d) {
			return
		}
	}
	if target.kind == fluidLava && fluidHolding(state).kind == fluidWater {
		// Lava's override comes first: stone over a pure water block, only the
		// fizz over water a container holds.
		if blocklogic.FluidOf(state).Pure {
			stone, ok := p.veto(HookForm, x, y, z, stoneState, from)
			if !ok {
				return
			}
			p.setShaped(x, y, z, stone, changes)
		}
		p.effects = append(p.effects, Effect{LevelEvent: 1501, X: x, Y: y, Z: z})
		return
	}
	if fluidContainer(state) {
		if target.kind == fluidWater && target.isSource() {
			if !registry.StateBool(state, registry.PropWaterlogged) {
				if flooded, sound, ok := blocklogic.PlaceWater(state); ok {
					p.setShaped(x, y, z, flooded, changes)
					p.scheduleFluid(x, y, z)
					if sound != registry.SoundNone {
						effect := Effect{SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 1}
						if registry.StateLit(state) {
							effect.GameEvent = gameEventBlockChange
						}
						p.effects = append(p.effects, effect)
					}
				}
			}
		}
		return
	}
	if state != airState {
		switch target.kind {
		case fluidWater:
			// WaterFluid.beforeDestroyingBlock: the replaced block drops its
			// loot. The block entity is read now, before the write removes it.
			tag, _ := p.w.BlockEntityTag(x, y, z)
			p.effects = append(p.effects, Effect{BreakDrop: state, BreakTag: tag, X: x, Y: y, Z: z})
		case fluidLava:
			// LavaFluid.beforeDestroyingBlock: the fizz and nothing else.
			p.effects = append(p.effects, Effect{LevelEvent: 1501, X: x, Y: y, Z: z})
		}
	}
	p.setShaped(x, y, z, target.createLegacy(), changes)
}

// shouldSpreadLiquid is lava's wake check: water beside or above (in a
// container too) makes obsidian over a source and cobblestone over a flow, with
// the fizz; soul soil below and blue ice beside make basalt. Water has no such
// check. The write runs the flags-3 shape pass, so an observer hears it.
// vanilla: LiquidBlock.shouldSpreadLiquid, Level.setBlock
func (p *Physics) shouldSpreadLiquid(x, y, z int32, changes *[]BlockChange) bool {
	state := p.w.BlockAt(x, y, z)
	fs := fluidHolding(state)
	if fs.kind != fluidLava {
		return true
	}
	overSoulSoil := p.w.BlockAt(x, y-1, z) == soulSoilState
	// Up, then north, south, west, east; the first water, or soul soil with
	// blue ice, wins.
	for _, d := range [5][3]int32{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		ns := p.w.BlockAt(x+d[0], y+d[1], z+d[2])
		if fluidHolding(ns).kind == fluidWater {
			made := cobblestoneState
			if fs.isSource() {
				made = obsidianState
			}
			return p.lavaHardens(x, y, z, made, [3]int32{x + d[0], y + d[1], z + d[2]}, changes)
		}
		if overSoulSoil && ns == blueIceState {
			return p.lavaHardens(x, y, z, basaltState, [3]int32{x + d[0], y + d[1], z + d[2]}, changes)
		}
	}
	return true
}

// fluidTick is one fluid visit. A source keeps its fill and spreads; a flow
// recomputes itself (newLiquid), drying to air when its feeder is gone. It then
// pours and floods; no further visit is booked for a cell that emptied, since
// the write that changes a cell wakes it again. Every write runs the flags-3
// shape pass, so an observer hears the level move.
// vanilla: FlowingFluid.tick, Level.setBlock
func (p *Physics) fluidTick(x, y, z int32, fs fluidState, changes *[]BlockChange) {
	if !fs.isSource() {
		newFS := p.newLiquid(x, y, z, fs.kind)
		if newFS.kind == fluidNone {
			p.setShaped(x, y, z, airState, changes)
			return
		}
		if newFS != fs {
			// vanilla: LiquidBlock.onPlace books the base delay if the fluid stays.
			// LevelChunkTicks.schedule keeps it over FlowingFluid.tick's later booking.
			p.setShaped(x, y, z, newFS.createLegacy(), changes)
		}
		fs = newFS
	}
	p.spread(x, y, z, fs, changes)
}

// lavaHardens writes the block lava becomes beside water or blue ice, with the
// fizz, and reports whether the lava stayed (a vetoed form leaves it as it was).
func (p *Physics) lavaHardens(x, y, z int32, made uint16, from [3]int32, changes *[]BlockChange) bool {
	state, ok := p.veto(HookForm, x, y, z, made, from)
	if !ok {
		return true
	}
	p.setShaped(x, y, z, state, changes)
	p.effects = append(p.effects, Effect{LevelEvent: 1501, X: x, Y: y, Z: z})
	return false
}
