package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The falling block, from the FallingBlockEntity bytecode. A plain
// value like items and primed TNT, returning events.
//
// X, Y, Z is the box's bottom centre; the box is FALLING_BLOCK's
// sized(0.98F, 0.98F).

// FallingBlockEntity's numbers: gravity 0.04 (getDefaultGravity), air drag
// 0.98 (getAirDrag, on the whole velocity after the move); a landing's
// multiply(0.7, -0.5, 0.7), as primed TNT; below the world's bottom the
// block drops after 100 ticks (64 below, onBelowWorld discards it with no
// drop), and after 600 ticks of falling it drops wherever it is.
const (
	fallingGravity      = 0.04
	fallingGroundDragXZ = 0.7
	fallingGroundBounce = -0.5

	FallingHalfWidth       = 0.49
	FallingHeight          = 0.98
	FallingTrackingRange   = 10
	FallingUpdateInterval  = 20
	fallingOutOfWorldTicks = 100
	fallingLifetimeTicks   = 600
	fallingFastVelocity    = 1.0
	fallingRayStep         = 0.25
)

// FallingEventKind is one thing a falling block's tick produced that the
// server may want to tell the world about or act on.
type FallingEventKind uint8

const (
	// FallingLanded: the body came to rest where it can place its block, at the
	// event's block position with State (concrete powder that reached water
	// names its concrete). The server sets the block, tells clients and
	// relights the column.
	FallingLanded FallingEventKind = iota
	// FallingDropped: the body came to rest where its block cannot be
	// placed, and vanilla discards it and drops the block as an item.
	FallingDropped
	// FallingHurt: the landing hurt the living bodies under it, with the damage
	// and the block's type (falling_anvil, falling_stalactite or falling_block)
	// for the server to apply. It precedes the same tick's landing or drop.
	// vanilla: FallingBlockEntity.causeFallDamage
	FallingHurt
	// FallingBroken: the landing broke the body, at the event's block
	// position with State; it precedes the drop, if any, and plays the
	// block's FallingBrokenEvent. A timed-out drop has none.
	// vanilla: FallingBlockEntity.callOnBrokenAfterFall
	FallingBroken
	// FallingSteppedOnRedstoneOre: the body rests on the unlit redstone ore at
	// X, Y, Z.
	FallingSteppedOnRedstoneOre
)

// FallingEvent is one event from a falling block's tick. X, Y, Z and State
// go with a landing, drop or break; Feet with a drop or break (the body's final position,
// which the item and level event use); Damage and DamageType with a hurt.
type FallingEvent struct {
	Kind                FallingEventKind
	X, Y, Z             int32
	State               uint16
	FeetX, FeetY, FeetZ float64
	Damage              float64
	DamageType          registry.DamageType
}

// FallingBlock is one gravity block in flight: its state, position,
// velocity and age, whether an unsurvivable landing drops it as an item, the
// hurt it deals, and the fall distance that hurt and anvil wear use.
type FallingBlock struct {
	ID int32
	Place
	VX, VY, VZ float64
	OnGround   bool
	stuck      stuckFactor
	// TicksFrozen is the powder snow count, up to rules.FullyFrozenTicks; only
	// lava takes it down.
	// vanilla: Entity.setTicksFrozen
	TicksFrozen int

	BlockState     uint16
	Time           int
	DropItem       bool
	HurtEntities   bool
	FallHurtMax    int
	FallHurtAmount float64
	CancelDrop     bool
	FallDistance   float64
	// FireTicks is vanilla's remainingFireTicks. The body's tick never runs
	// baseTick, so lava sets it and nothing counts it down or draws flames.
	FireTicks int
	// LandedFall is the fall a landing this tick ended, 0 when none.
	LandedFall float64
	// vanilla: Entity.isNoGravity
	NoGravity bool

	// StartX, StartY and StartZ are the cell the fall began in, vanilla's
	// DATA_START_POS: set by fall() alone, (0, 0, 0) on a loaded body.
	StartX, StartY, StartZ int32

	Rand *rand.Rand

	Dead bool
}

// NewFallingBlock places a falling block carrying blockState at
// FallingBlockEntity.fall's spawn point (the cell's centre on x and z, its
// bottom on y). The state carries no water: fall() drops waterlogged. Its
// random source is seeded with seed.
func NewFallingBlock(id int32, x, y, z, vx, vy, vz float64, blockState uint16, seed int64) *FallingBlock {
	return &FallingBlock{
		ID: id,
		X:  x, Y: y, Z: z,
		VX: vx, VY: vy, VZ: vz,
		BlockState: blockState,
		DropItem:   true,
		Rand:       NewDice(seed),
	}
}

// SetHurtsEntities is FallingBlockEntity.setHurtsEntities (called by the
// anvil's falling): the landing hurts what it hits at amount per block,
// capped at maxDamage. The numbers are saved with the body and
// causeFallDamage turns them into the reported hurt.
func (f *FallingBlock) SetHurtsEntities(amount float64, maxDamage int) {
	f.HurtEntities = true
	f.FallHurtAmount = amount
	f.FallHurtMax = maxDamage
}

// isFallingAnvil is the anvil family, whose landing wears the anvil.
func isFallingAnvil(b registry.Block) bool {
	return b == registry.BlockAnvil || b == registry.BlockChippedAnvil || b == registry.BlockDamagedAnvil
}

// isFallingStalactite is the speleothems dealing stalactite damage
// (SpeleothemBlock.getFallDamageSource): pointed dripstone and its sulfur twin.
func isFallingStalactite(b registry.Block) bool {
	return b == registry.BlockPointedDripstone || b == registry.BlockSulfurSpike
}

// fallingDamageType is FallingBlockEntity.getFallDamageSource: the anvil
// type, the stalactite type, else falling_block.
func fallingDamageType(b registry.Block) registry.DamageType {
	switch {
	case isFallingAnvil(b):
		return rules.DamageTypeFallingAnvil
	case isFallingStalactite(b):
		return rules.DamageTypeFallingStalactite
	default:
		return rules.DamageTypeFallingBlock
	}
}

// causeFallDamage reports the landing's hurt (per-block amount capped at
// FallHurtMax, typed by the block) and rolls anvil wear. The wear is rolled
// first, so the worn block is what gets placed; an anvil worn past its last
// state cancels the drop, which the landing honours.
// vanilla: FallingBlockEntity.causeFallDamage
func (f *FallingBlock) causeFallDamage() []FallingEvent {
	if !f.HurtEntities {
		return nil
	}
	i := math.Ceil(f.FallDistance - 1)
	if i < 0 {
		return nil
	}
	damage := math.Min(math.Floor(float64(float32(i)*float32(f.FallHurtAmount))), float64(f.FallHurtMax))
	block := registry.BlockOf(f.BlockState)
	if isFallingAnvil(block) && damage > 0 && f.dice().Float64() < float64(0.05+float32(i)*0.05) {
		next, gone := registry.DamageAnvilState(f.BlockState)
		if gone {
			f.CancelDrop = true
		} else {
			f.BlockState = next
		}
	}
	if damage <= 0 {
		return nil
	}
	return []FallingEvent{{Kind: FallingHurt, Damage: damage, DamageType: fallingDamageType(block)}}
}

// HurtBox is the landing hurt's area, the body's own 0.98 box.
// vanilla: FallingBlockEntity.causeFallDamage, getBoundingBox
func (f *FallingBlock) HurtBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return f.X - FallingHalfWidth, f.Y, f.Z - FallingHalfWidth,
		f.X + FallingHalfWidth, f.Y + FallingHeight, f.Z + FallingHalfWidth
}

// dice is the body's own random source, made from its ID when the one the
// constructor gave has been left unset.
func (f *FallingBlock) dice() *rand.Rand {
	if f.Rand == nil {
		f.Rand = NewDice(int64(f.ID))
	}
	return f.Rand
}

// Step advances the falling block one tick (FallingBlockEntity.tick):
// gravity and the move, then a landing (on the ground, or concrete powder
// reaching water) places the block or drops it as an item and ends the
// body; falling past the bottom or too long drops it too. A survivor has air
// drag applied.
func (f *FallingBlock) Step(w World) []FallingEvent {
	if f.Dead {
		return nil
	}
	// vanilla: FallingBlockEntity.tick opens with BlockState.isAir(), which
	// holds for air, cave_air and void_air.
	if world.IsAirState(f.BlockState) {
		f.Dead = true
		return nil
	}
	f.Time++

	// The base tick's below-world check: gone, placing and dropping nothing.
	if f.Y < float64(w.MinY())-64 {
		f.Dead = true
		return nil
	}

	if !f.NoGravity {
		f.VY -= fallingGravity
	}
	oldX, oldY, oldZ := f.X, f.Y, f.Z
	ax, az := f.VX, f.VZ
	f.OnGround = f.move(w)
	freezeInside(w, bodyMove{fx: oldX, fy: oldY, fz: oldZ, tx: f.X, ty: f.Y, tz: f.Z},
		AABB{-FallingHalfWidth, 0, -FallingHalfWidth, FallingHalfWidth, FallingHeight, FallingHalfWidth}, &f.TicksFrozen)
	var events []FallingEvent
	if f.OnGround {
		if x, y, z, ok := unlitOreUnder(w, f.X, f.Y, f.Z, FallingHalfWidth); ok {
			events = append(events, FallingEvent{Kind: FallingSteppedOnRedstoneOre, X: x, Y: y, Z: z})
		}
	}

	// The landing cell, and for concrete powder whether it holds water. A
	// powder falling over a block a tick may have crossed water, so vanilla
	// clips a ray from the old position to the new.
	blockX, blockY, blockZ := f.blockPosition()

	// checkFallDamage: a downward move adds its distance (in vanilla's
	// float), water at the feet adds nothing (the cell standing in for
	// vanilla's box test). A landing asks the fall damage (and anvil wear)
	// and resets the distance. A step through a web or water clears the
	// carried fall first.
	// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING
	if rules.FallResetSweep(w, f.FallDistance, player.Position{X: oldX, Y: oldY, Z: oldZ},
		player.Position{X: f.X, Y: f.Y, Z: f.Z}, false, false) {
		f.FallDistance = 0
	}
	dyMoved := f.Y - oldY
	if dyMoved < 0 && fluidCellOf(w.BlockAt(blockX, blockY, blockZ)).kind != fluidWater {
		f.FallDistance -= float64(float32(dyMoved))
	}
	// The column's push comes after the fall is counted, so a body inside
	// one forgets its distance.
	// vanilla: Entity.onInsideBubbleColumn
	// vanilla: LavaFluid.entityInside, Entity.lavaIgnite
	if BoxInLava(w, f.X, f.Y, f.Z, 2*FallingHalfWidth, FallingHeight) {
		f.FireTicks = max(f.FireTicks, 300)
	}
	var spots []bubbleSpot
	rel := AABB{-FallingHalfWidth, 0, -FallingHalfWidth, FallingHalfWidth, FallingHeight, FallingHalfWidth}
	walkInside(w, []bodyMove{{oldX, oldY, oldZ, f.X, f.Y, f.Z, ax, az, true}}, rel,
		func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
			if vy, ok := projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, f.VY, true, &spots); ok {
				f.VY = vy
				if len(spots) == 0 {
					f.FallDistance = 0
				}
				spots = spots[:0]
			}
			return true
		})
	// The landing's hurt of the bodies under it, kept to ride out with
	// the landing or the drop the same tick reaches.
	f.LandedFall = 0
	if f.OnGround {
		f.LandedFall = f.FallDistance
		events = append(events, f.causeFallDamage()...)
		f.FallDistance = 0
	}

	concrete, isPowder := registry.BlockOf(f.BlockState).Concrete()
	inWater := false
	if isPowder {
		if fluidCellOf(w.BlockAt(blockX, blockY, blockZ)).kind == fluidWater {
			inWater = true
		} else if f.VX*f.VX+f.VY*f.VY+f.VZ*f.VZ > fallingFastVelocity {
			if hit, ok := f.rayWater(w, oldX, oldY, oldZ); ok {
				blockX, blockY, blockZ = hit[0], hit[1], hit[2]
				inWater = true
			}
		}
	}

	if f.OnGround || inWater {
		// The landing's damping, taken before anything else the tick
		// decides.
		f.VX *= fallingGroundDragXZ
		f.VY *= fallingGroundBounce
		f.VZ *= fallingGroundDragXZ
		if f.CancelDrop {
			// cancelDrop breaks the body regardless of the drop rules.
			f.Dead = true
			return append(events, f.brokenEvent(blockX, blockY, blockZ))
		}
		// A speleothem's landing is a break (SpeleothemBlock.onBrokenAfterFall):
		// vanilla places it and drops it when canSurvive fails with air above,
		// so the drop is taken directly.
		if isFallingStalactite(registry.BlockOf(f.BlockState)) {
			f.Dead = true
			return append(events, f.dropEvent(w, true, blockX, blockY, blockZ)...)
		}
		// The place test: the cell must be replaceable and the one below not
		// free to fall through (empty-hand canBeReplaced, so snow of 2+ layers refuses), except for powder that reached water, the one
		// case vanilla places over a free cell.
		heldUp := !fallingFree(w.BlockAt(blockX, blockY-1, blockZ)) || (isPowder && inWater)
		if rules.CanBeReplaced(w.BlockAt(blockX, blockY, blockZ), registry.BlockAir, rules.PlacementContext{Face: protocol.FaceTop}) && heldUp {
			// The carried block, waterlogged if the cell holds a water source and
			// the block can hold some; powder that reached water becomes its
			// concrete.
			// vanilla: FallingBlockEntity.tick
			state := f.BlockState
			if cell := fluidCellOf(w.BlockAt(blockX, blockY, blockZ)); cell.kind == fluidWater && cell.level == 0 {
				if wet, ok := registry.WithBool(state, registry.PropWaterlogged, true); ok {
					state = wet
				}
			}
			if isPowder && (inWater || world.ConcretePowderTouchesLiquid(w, blockX, blockY, blockZ)) {
				state = concrete.DefaultState()
			}
			if w.PlacementSurvives(state, blockX, blockY, blockZ) {
				f.Dead = true
				return append(events, FallingEvent{Kind: FallingLanded, X: blockX, Y: blockY, Z: blockZ, State: state})
			}
		}
		// The place test or canSurvive failed: vanilla discards the body and
		// drops the block as an item where it stood.
		f.Dead = true
		return append(events, f.dropEvent(w, true, blockX, blockY, blockZ)...)
	}

	// Fall time limits: below the bottom it drops after 100 ticks, in the air
	// after 600 wherever it is. (Vanilla also drops one above the dimension's
	// top, which this server cannot place a block in to begin with.)
	if (f.Time > fallingOutOfWorldTicks && blockY <= w.MinY()) || f.Time > fallingLifetimeTicks {
		f.Dead = true
		return append(events, f.dropEvent(w, false, blockX, blockY, blockZ)...)
	}

	// Air drag at the end of the tick, so the velocity the move used (and the
	// motion packets carry) is the undragged one.
	f.VX *= airDragFloat
	f.VY *= airDragFloat
	f.VZ *= airDragFloat
	return events
}

// dropEvent is the drop of a body giving up its block: the item at its feet
// (spawnAtLocation), when it drops at all and entity_drops allows; a landing
// (broken) breaks it at the cell x, y, z first, a timeout does not.
// vanilla: FallingBlockEntity.tick
func (f *FallingBlock) dropEvent(w World, broken bool, x, y, z int32) []FallingEvent {
	if !f.DropItem || !w.BoolRule(world.RuleEntityDrops) {
		return nil
	}
	drop := FallingEvent{Kind: FallingDropped, State: f.BlockState, FeetX: f.X, FeetY: f.Y, FeetZ: f.Z}
	if !broken {
		return []FallingEvent{drop}
	}
	return []FallingEvent{f.brokenEvent(x, y, z), drop}
}

// brokenEvent is the break of a body at the landing cell x, y, z.
func (f *FallingBlock) brokenEvent(x, y, z int32) FallingEvent {
	return FallingEvent{Kind: FallingBroken, X: x, Y: y, Z: z, State: f.BlockState,
		FeetX: f.X, FeetY: f.Y, FeetZ: f.Z}
}

// blockPosition is the entity's own blockPosition, the cell its feet stand
// in.
func (f *FallingBlock) blockPosition() (int32, int32, int32) {
	return int32(math.Floor(f.X)), int32(math.Floor(f.Y)), int32(math.Floor(f.Z))
}

// rayWater walks the cells the move crossed and reports the first water (a
// fast concrete powder's); a solid cell first ends it with nothing, as
// vanilla's clip stops at the block it hits.
func (f *FallingBlock) rayWater(w World, oldX, oldY, oldZ float64) ([3]int32, bool) {
	dx, dy, dz := f.X-oldX, f.Y-oldY, f.Z-oldZ
	steps := int(math.Max(math.Abs(dy), math.Max(math.Abs(dx), math.Abs(dz)))/fallingRayStep) + 1
	for i := 1; i <= steps; i++ {
		frac := float64(i) / float64(steps)
		x := int32(math.Floor(oldX + dx*frac))
		y := int32(math.Floor(oldY + dy*frac))
		z := int32(math.Floor(oldZ + dz*frac))
		state := w.BlockAt(x, y, z)
		if fluidCellOf(state).kind == fluidWater {
			return [3]int32{x, y, z}, true
		}
		if cellExtent(w, state, x, y, z) > 0 {
			return [3]int32{}, false
		}
	}
	return [3]int32{}, false
}

// fallingFree is FallingBlock.isFree, used by both the fall trigger and the
// landing's below check: air, the fire tag, a water or lava block, or a
// replaceable. The cell's fluid alone does not count: a waterlogged solid
// holds a falling block.
// vanilla: FallingBlock.isFree
func fallingFree(state uint16) bool {
	return registry.CanFallThrough(state)
}

// move moves the body along its velocity as far as blocks allow, vertical
// first then horizontal larger first, as vanilla's MoverType walk does. It
// reports whether it ended on the ground.
func (f *FallingBlock) move(w World) bool {
	x, y, z := f.X, f.Y, f.Z
	onGround := moveBox(w, f.collideCtx(), &x, &y, &z, &f.VX, &f.VY, &f.VZ, &f.stuck, FallingHalfWidth, FallingHeight)
	f.SetPos(x, y, z)
	return onGround
}

// EyePosition is getEyePosition at the type's default eye height (0.98 * 0.85,
// float32), where an explosion's knockback is directed from.
func (f *FallingBlock) EyePosition() (x, y, z float64) {
	return f.X, f.Y + float64(float32(FallingHeight)*float32(0.85)), f.Z
}

// BlastBox is the bounding box the exposure of a blast is measured over —
// the body's own ninety-eight-hundredths box, getSeenPercent's shape.
func (f *FallingBlock) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return f.X - FallingHalfWidth, f.Y, f.Z - FallingHalfWidth,
		f.X + FallingHalfWidth, f.Y + FallingHeight, f.Z + FallingHalfWidth
}

// Push is Entity.push, the velocity an explosion's knockback adds onto.
func (f *FallingBlock) Push(vx, vy, vz float64) {
	f.VX += vx
	f.VY += vy
	f.VZ += vz
}

// collideCtx is the context of the powder snow's collision for this body.
func (f *FallingBlock) collideCtx() collideCtx {
	return collideCtx{fallDistance: f.FallDistance, fallingBlock: true}
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (f *FallingBlock) CapFall() { CapFallAccumulation(f.VY, &f.FallDistance) }
