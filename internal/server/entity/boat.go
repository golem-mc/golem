package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The boat: vanilla's AbstractBoat as this server runs it (box, hurt state,
// an unridden hull's float). A plain value like primed TNT and arrows:
// nothing is sent or locked, and the server reads the state the tick
// leaves.

// Boat numbers: the EntityTypes.BOAT box sized(1.375F, 0.5625F);
// Entity.getDefaultGravity's 0.04; ground friction from the block's
// slipperiness (0.6 when it has none) and the base tick's 0.9 air drag,
// per axis, applied before the move.
const (
	BoatHalfWidth = rules.BoatHalfWidth
	BoatHeight    = rules.BoatHeight
	BoatSeatY     = rules.BoatSeatY

	boatGravity     = 0.04
	boatAirFriction = 0.9

	// AbstractBoat.floatBoat: lift is the water's depth over the hull as a
	// share of its height, against gravity; vertical speed is scaled by 0.75
	// each tick and horizontal drift loses a tenth in water. A fully submerged
	// hull gets a flat 0.01 lift instead and drags half again as hard.
	boatWaterDamp          = 0.75
	boatWaterFriction      = 0.9
	boatUnderWaterLift     = 0.01
	boatUnderWaterFriction = 0.45

	// boatBuoyancyDiv is the drowned share of the hull's height where lift
	// balances gravity: at rest a boat floats 0.65 of a hull below the surface.
	boatBuoyancyDiv = 0.65

	// boatUnderFlowingSink is the flat pull down on a hull wholly under running
	// water, instead of gravity and lift.
	// vanilla: AbstractBoat.floatBoat's UNDER_FLOWING_WATER
	boatUnderFlowingSink = 7e-4

	// boatEntryDepth is where a hull dropping from air into water is snapped
	// to: the whole hull less 0.101.
	// vanilla: AbstractBoat.tick's IN_AIR → IN_WATER
	boatEntryDepth = BoatHeight - 0.101

	// boatOutOfControlTicks is the time under water that unseats the riders.
	// vanilla: AbstractBoat.tick
	boatOutOfControlTicks = 60

	// The ticks a blow's rocking runs for, vanilla's setHurtTime(10).
	BoatHurtTicks = 10

	// BoatHurtBreakTotal is VehicleEntity.hurtServer's 40.0f break threshold.
	// The total wears off by one a tick, so widely spaced blows never add up.
	BoatHurtBreakTotal = 40.0
)

// EntityTypes.OAK_BOAT: clientTrackingRange(10), and the builder's default
// updateInterval of 3.
const (
	BoatTrackingRange  = 10
	BoatUpdateInterval = 3
)

type boatStatus uint8

const (
	boatStatusNone boatStatus = iota
	boatInWater
	boatUnderWater
	boatUnderFlowingWater
	boatOnLand
	boatInAir
)

// Boat is one boat in a world. HurtTime, HurtDir and HurtDamage are its
// metadata: rocking ticks left, the rocking direction (flipped every blow),
// and the accumulated damage, which wears off by one a tick and breaks the
// boat past 40. NoLoot marks an end that drops no boat item (a creative
// blow, or below the world); a chest boat's cargo spills regardless. Dead
// marks one the server's next pass removes.
type Boat struct {
	stride stepState
	// Movement is the vibration produced by the last Step.
	Movement MovementEvent
	waterEntry

	// Splash is the water entry of the last Step, nil for none.
	Splash *WaterSplash
	// bounced marks a landing that sent the boat back up; TakeBounce reads it.
	bounced bool

	ID int32
	Place
	VX, VY, VZ float64
	Yaw        float32
	OnGround   bool
	// fallDistance is the fall banked since the hull last stood on the ground.
	fallDistance float64
	// vanilla: Entity.isNoGravity
	NoGravity bool
	// Invulnerable is the permanent Invulnerable save flag.
	Invulnerable bool

	HurtTime   int32
	HurtDir    int32
	HurtDamage float64

	LeftPaddle  bool
	RightPaddle bool
	// paddlePos is each paddle's swing angle in radians; a side with its paddle
	// off rests at 0. vanilla: AbstractBoat.paddlePositions
	paddlePos [2]float32
	// Paddles are the paddle sounds of the last Step.
	Paddles []PaddleSound

	water, lava fluidTracker

	// A new or loaded hull has no previous status, so only a fall from air snaps.
	lastStatus boatStatus
	// lastYd is the last move's vertical speed, bounding the water surface scan.
	lastYd float64

	// stuck is the multiplier the last block effects (InsideBlocks) left
	// for the next move.
	stuck stuckFactor
	// stepMove is the move the last Step made, valid while stepMoved: an
	// unridden hull's. A ridden one is moved by its driver instead.
	stepMove  bodyMove
	stepMoved bool

	// BubbleTime counts the ticks the hull has ridden a column's surface;
	// bubbleDrag is the direction of the last surface touched.
	// vanilla: AbstractBoat.getBubbleTime
	BubbleTime  int
	aboveBubble bool
	bubbleDrag  bool
	// EjectPassengers is set when a downward column throw shook the riders
	// loose; the server dismounts them and clears it.
	// vanilla: AbstractBoat.tickBubbleColumn
	EjectPassengers bool
	// Thrown is the throw this tick's column spell ended in; the server
	// tells it and clears it.
	Thrown BoatThrow

	// outOfControl counts ticks fully under water; at 60 the riders are
	// dismounted and boarding is refused.
	// vanilla: AbstractBoat.outOfControlTicks
	outOfControl int

	NoLoot bool
	Dead   bool
	// LavaHurts and Extinguishes count this tick's sounds.
	LavaHurts, Extinguishes int
	// DousedCauldrons are the layered cauldrons a burning boat was put out in.
	DousedCauldrons [][3]int32
	// FireTicks is the burn left, 0 for none; Burning is the on-fire flag the
	// clients are shown, set as of this tick's start.
	// vanilla: Entity.remainingFireTicks, Entity.setSharedFlagOnFire
	FireTicks int
	Burning   bool
}

// NewBoat places a still, unhurt boat at the position facing yaw. The
// direction count starts at the metadata default of 1.
func NewBoat(id int32, x, y, z float64, yaw float32) *Boat {
	return &Boat{
		ID: id,
		X:  x, Y: y, Z: z,
		Yaw:     yaw,
		HurtDir: 1,
	}
}

// BoatKind is the boat kind an entity type name names (oak, chest boat), or
// "": every boat and raft entity is named after its item, and the family is
// read from the boats item tag.
func BoatKind(name string) string {
	if _, ok := registry.EntityTypeID(name); !ok {
		return ""
	}
	item, ok := registry.ItemID(name)
	if !ok || !registry.Item(item).InTag(registry.ItemTagIDBoats) {
		return ""
	}
	return name
}

// gravity is Entity.getGravity: 0.04, or 0 when Entity.isNoGravity.
func (b *Boat) gravity() float64 {
	if b.NoGravity {
		return 0
	}
	return boatGravity
}

// Step advances the boat one tick (AbstractBoat.tick, with driving left to
// the rider's client): unridden it floats and falls, ridden it stays where
// the driver puts it, and the rocking and the column spell run either way.
// vanilla: AbstractBoat.tick, with the driving a rider's client runs
func (b *Boat) Step(w World, ridden bool) {
	b.stepMoved = false
	b.Movement = MovementEvent{}
	b.Splash = nil
	b.Paddles = b.Paddles[:0]
	if b.Dead {
		return
	}
	// The base tick's below-world check: 64 below the floor, onBelowWorld
	// discards it with no boat item, the cargo spilling.
	if b.Y < float64(w.MinY())-64 {
		b.Dead = true
		b.NoLoot = true
		return
	}
	b.Splash = b.step(w, b.boatBox(), b.VX, b.VY, b.VZ)
	// Rocking and its accumulated damage both run down a tick.
	if b.HurtTime > 0 {
		b.HurtTime--
	}
	if b.HurtDamage > 0 {
		b.HurtDamage--
	}
	// Fluids are read even while ridden: a submerged bow is what refuses a
	// would-be passenger. Currents push only a hull no client drives.
	b.updateFluids(w, !ridden)
	b.burnTick()
	previous := b.lastStatus
	status, friction := b.status(w)
	b.lastStatus = status
	under := status == boatUnderWater || status == boatUnderFlowingWater
	b.tickOutOfControl(under)
	inWater := b.water.height > 0
	if ridden {
		b.bubbleColumn(w, true)
		b.tickPaddles(w, under || inWater)
		return
	}

	// vanilla: AbstractBoat.floatBoat
	if previous == boatInAir && status != boatInAir && status != boatOnLand {
		targetY := b.waterLevelAbove(w) - boatEntryDepth
		if !overlapsSolid(w, fallCtx(b.fallDistance), b.boatBox().moved(0, targetY-b.Y, 0)) {
			b.Y, b.VY, b.lastYd = targetY, 0, 0
		}
		b.lastStatus = boatInWater
		inWater = true
	} else {
		// The status sets the drag before the move, as floatBoat does: fully
		// submerged, a flat 0.01 lift and half again the drag; under running water,
		// a tenth per axis and a flat 7e-4 down; partly in, a tenth and lift by
		// depth; in air, a tenth; on the ground, the block's slipperiness.
		switch status {
		case boatUnderFlowingWater:
			b.VX *= boatWaterFriction
			b.VZ *= boatWaterFriction
			b.VY -= boatUnderFlowingSink
		case boatUnderWater:
			b.VX *= boatUnderWaterFriction
			b.VZ *= boatUnderWaterFriction
			b.VY = (b.VY - b.gravity() + boatUnderWaterLift*boatGravity/boatBuoyancyDiv) * boatWaterDamp
		case boatInWater:
			depth := b.water.height / BoatHeight
			b.VX *= boatWaterFriction
			b.VZ *= boatWaterFriction
			b.VY = (b.VY - b.gravity() + depth*boatGravity/boatBuoyancyDiv) * boatWaterDamp
		case boatOnLand:
			b.VX *= friction
			b.VZ *= friction
			b.VY -= b.gravity()
		default:
			b.VX *= boatAirFriction
			b.VZ *= boatAirFriction
			b.VY -= b.gravity()
		}
	}

	fx, fy, fz, ax, az := b.X, b.Y, b.Z, b.VX, b.VZ
	b.OnGround = b.move(w, ridden)
	if b.OnGround {
		// AbstractBoat.tick applies the block effects twice.
		// vanilla: Entity.applyEffectsFromBlocks's stepOn
		for range 2 {
			slimeStep(w, b.X, b.Y, b.Z, BoatHalfWidth, b.VY, &b.VX, &b.VZ)
		}
	}
	b.Movement = b.stride.vehicleEvent(w, bodyMove{fx: fx, fy: fy, fz: fz, tx: b.X, ty: b.Y, tz: b.Z}, BoatHalfWidth, b.OnGround, false, inWater)
	b.stepMove, b.stepMoved = bodyMove{fx, fy, fz, b.X, b.Y, b.Z, ax, az, true}, true
	b.bubbleColumn(w, false)
	b.tickPaddles(w, under || inWater)
}

// status reads water before ground contact, even for a hull on the bottom.
// vanilla: AbstractBoat.getStatus
func (b *Boat) status(w World) (boatStatus, float64) {
	under, flowing := b.waterStatus(w)
	if flowing {
		return boatUnderFlowingWater, 0
	}
	if under {
		return boatUnderWater, 0
	}
	if b.water.height > 0 {
		return boatInWater, 0
	}
	if friction, ok := b.groundFriction(w); ok && friction > 0 {
		return boatOnLand, friction
	}
	return boatInAir, 0
}

// waterLevelAbove finds the surface above a hull that may have fallen through
// several water cells in its last move.
// vanilla: AbstractBoat.getWaterLevelAbove
func (b *Boat) waterLevelAbove(w World) float64 {
	box := b.boatBox()
	maxY := int32(math.Ceil(box.maxY - b.lastYd))
layers:
	for y := int32(math.Floor(box.maxY)); y < maxY; y++ {
		var height float64
		for x := int32(math.Floor(box.minX)); x < int32(math.Ceil(box.maxX)); x++ {
			for z := int32(math.Floor(box.minZ)); z < int32(math.Ceil(box.maxZ)); z++ {
				cell := fluidCellOf(w.BlockAt(x, y, z))
				if cell.kind == fluidWater {
					height = max(height, fluidHeightAt(w, x, y, z, cell))
					if height >= 1 {
						continue layers
					}
				}
			}
		}
		return float64(y) + height
	}
	return float64(maxY + 1)
}

// PaddleSound is one paddle stroke's sound, played at X, Y, Z.
type PaddleSound struct {
	X, Y, Z float64
	// Water picks entity.boat.paddle_water over paddle_land.
	Water bool
	Pitch float32
}

// tickPaddles swings each turned paddle a step and queues the stroke's sound
// as it passes the start of its arc; a paddle turned off rests at 0. Nothing
// sounds in the air.
// vanilla: AbstractBoat.tick, getPaddleSound
func (b *Boat) tickPaddles(w World, water bool) {
	if !b.LeftPaddle && !b.RightPaddle {
		b.paddlePos = [2]float32{}
		return
	}
	playable := water
	if !water && b.OnGround {
		_, playable = b.groundFriction(w)
	}
	vx, _, vz := LookVector(b.Yaw, 0)
	for i, on := range [2]bool{b.LeftPaddle, b.RightPaddle} {
		if !on {
			b.paddlePos[i] = 0
			continue
		}
		pos := b.paddlePos[i]
		if playable && float32(math.Mod(float64(pos), 2*math.Pi)) <= math.Pi/4 &&
			float32(math.Mod(float64(pos+math.Pi/8), 2*math.Pi)) >= math.Pi/4 {
			dx, dz := vz, -vx
			if i == 1 {
				dx, dz = -vz, vx
			}
			b.Paddles = append(b.Paddles, PaddleSound{
				X: b.X + dx, Y: b.Y, Z: b.Z + dz, Water: water, Pitch: 0.8 + 0.4*rand.Float32(),
			})
		}
		b.paddlePos[i] = pos + 0.3926991
	}
}

// updateFluids reads water and lava around the hull and adds the currents'
// push, the generic fluid pass.
// vanilla: Entity.updateFluidHeightAndDoFluidPushing
func (b *Boat) updateFluids(w World, withCurrent bool) {
	// The eye is at the hull's top: a boat takes passengers only while its
	// bow is above water.
	updateFluid(w, b.boatBox(), b.X, b.Y+BoatHeight, b.Z, withCurrent, &b.water, &b.lava)
	if !withCurrent {
		return
	}
	b.VX, b.VY, b.VZ = applyCurrents(w, &b.water, &b.lava, b.VX, b.VY, b.VZ)
}

// boatBox is the hull's span as a fluid-walk AABB.
func (b *Boat) boatBox() AABB {
	return AABB{
		minX: b.X - BoatHalfWidth, minY: b.Y, minZ: b.Z - BoatHalfWidth,
		maxX: b.X + BoatHalfWidth, maxY: b.Y + BoatHeight, maxZ: b.Z + BoatHalfWidth,
	}
}

// waterStatus is AbstractBoat.isUnderwater: it reads only the cells from
// floor(maxY) to ceil(maxY + 0.001), counting water only when its surface
// is above maxY + 0.001. under means still source water over the ceiling,
// flowing a running cell there, from the same scan.
// vanilla: AbstractBoat.isUnderwater
func (b *Boat) waterStatus(w World) (under, flowing bool) {
	maxY := b.Y + BoatHeight
	d := maxY + 0.001
	for x := int32(math.Floor(b.X - BoatHalfWidth)); x < int32(math.Ceil(b.X+BoatHalfWidth)); x++ {
		for y := int32(math.Floor(maxY)); y < int32(math.Ceil(d)); y++ {
			for z := int32(math.Floor(b.Z - BoatHalfWidth)); z < int32(math.Ceil(b.Z+BoatHalfWidth)); z++ {
				cell := fluidCellOf(w.BlockAt(x, y, z))
				if cell.kind != fluidWater {
					continue
				}
				if d < float64(y)+fluidHeightAt(w, x, y, z, cell) {
					if cell.level != 0 {
						return true, true
					}
					under = true
				}
			}
		}
	}
	return under, flowing
}

// tickOutOfControl counts ticks under water (or resets) and asks the
// server to dismount riders from the 60th.
// vanilla: AbstractBoat.tick
func (b *Boat) tickOutOfControl(sunk bool) {
	if !sunk {
		b.outOfControl = 0
		return
	}
	b.outOfControl++
	if b.outOfControl >= boatOutOfControlTicks {
		b.EjectPassengers = true
	}
}

// EyeInWater reports isEyeInFluid(WATER) for the hull, which
// canAddPassenger refuses.
func (b *Boat) EyeInWater() bool { return b.water.eyesIn }

// CanTakePassenger is canAddPassenger: a free seat, the eye above water,
// and not sunk for 60 ticks.
func (b *Boat) CanTakePassenger(seated, capacity int) bool {
	return seated < capacity && !b.EyeInWater() && b.outOfControl < boatOutOfControlTicks
}

// TakeDamage is VehicleEntity.hurtServer for a boat: rock for ten ticks,
// flip the direction, and add ten times the damage to the total. A creative
// blow discards it with no item; a survival blow past 40 breaks it, dropping
// the boat item. It reports whether the blow landed.
// vanilla: VehicleEntity.hurtServer
func (b *Boat) TakeDamage(amount float64, creative bool) bool {
	return b.Hurt(amount, rules.DamageTypeGeneric, creative)
}

// Hurt applies a typed hurt, including the permanent invulnerability gate.
// vanilla: VehicleEntity.hurtServer, Entity.isInvulnerableToBase
func (b *Boat) Hurt(amount float64, damageType registry.DamageType, creative bool) bool {
	if b.Dead || (b.Invulnerable && !creative && !rules.InvulnerabilityBypassed(damageType)) {
		return false
	}
	b.HurtDir = -b.HurtDir
	b.HurtTime = BoatHurtTicks
	b.HurtDamage += amount * 10
	if creative {
		b.Dead = true
		b.NoLoot = true
	} else if b.HurtDamage > BoatHurtBreakTotal {
		b.Dead = true
	}
	return true
}

// BoatTarget is a boat as a projectile target: the hull's box and the
// kind's entity type ID for weapon enchantment conditions. A standing boat
// is pickable.
// vanilla: AbstractBoat.isPickable
func BoatTarget(id, entityType int32, b *Boat) ArrowTarget {
	minX, minY, minZ, maxX, maxY, maxZ := b.Box()
	return ArrowTarget{
		EntityID: id,
		Type:     entityType,
		MinX:     minX, MinY: minY, MinZ: minZ,
		MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		RootVehicle: id,
	}
}

// Box is the sized(1.375F, 0.5625F) box about the position, for blow reach
// and placement collision.
func (b *Boat) Box() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return b.X - BoatHalfWidth, b.Y, b.Z - BoatHalfWidth,
		b.X + BoatHalfWidth, b.Y + BoatHeight, b.Z + BoatHalfWidth
}

// Push is Entity.push for an explosion's knockback; the hull's tick sends
// it.
// vanilla: Entity.push
func (b *Boat) Push(vx, vy, vz float64) {
	b.VX += vx
	b.VY += vy
	b.VZ += vz
}

// EyePosition is Entity.getEyePosition at the boat types' eyeHeight (the
// hull's top), where an explosion's knockback is directed from.
// vanilla: Entity.getEyePosition, EntityTypes BOAT eyeHeight
func (b *Boat) EyePosition() (x, y, z float64) {
	return b.X, b.Y + BoatHeight, b.Z
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (b *Boat) CapFall() { CapFallAccumulation(b.VY, &b.fallDistance) }

// BlastBox is Box, the name the other kinds' snapshots read.
func (b *Boat) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return b.Box()
}

// move moves the boat along its velocity as far as blocks and collidable
// bodies allow: vertical, then horizontal larger first
// (Direction.axisStepOrder), zeroing stopped axes. A falling hull rests on
// the stopping shape's top; as in Entity.collide a hull already overlapping
// a body moves out freely instead of being lifted onto it. It reports
// whether the boat ended on the ground.
// vanilla: Entity.move, Entity.collideWithShapes, Direction.axisStepOrder,
// AbstractBoat.canVehicleCollide
func (b *Boat) move(w World, ridden bool) bool {
	onGround := false
	ctx, startY := fallCtx(b.fallDistance), b.Y
	// vanilla: Entity.move spends the stuck multiplier and the velocity.
	dx, dy, dz, held := b.stuck.scale(b.VX, b.VY, b.VZ)
	if held {
		b.VX, b.VY, b.VZ = 0, 0, 0
	}
	b.lastYd = b.VY
	landingVY := b.VY
	if dy != 0 {
		out, _, blocked := clipMove(w, ctx, b.boatBox(), axisY, dy)
		b.Y = out.minY
		if dy < 0 {
			onGround = blocked
		}
		if blocked {
			b.VY = 0
		}
	}
	if dx != 0 || dz != 0 {
		out, _, blockedX, blockedZ := collideHorizontal(w, ctx, b.boatBox(), dx, dz)
		b.SetPos(out.minX+BoatHalfWidth, b.Y, out.minZ+BoatHalfWidth)
		if blockedX {
			b.VX = 0
		}
		if blockedZ {
			b.VZ = 0
		}
	}
	// Support is identified at the final position, after horizontal collisions.
	// A boat a player steers is moved by that client, which keeps no restitution.
	// vanilla: Entity.move, Entity.canSimulateMovement
	if onGround && !ridden {
		// vanilla: AbstractBoat.getAirDrag is 1.0
		b.VY = bounceOffGround(w, b.X, b.Y, b.Z, BoatHalfWidth, landingVY, b.Y-startY, b.gravity(), 1, false)
		if b.VY > 0 {
			b.bounced = true
		}
	}
	// vanilla: Entity.move ends with the block speed factor
	f := blockSpeedFactor(w, b.X, b.Y, b.Z)
	b.VX *= f
	b.VZ *= f
	bankFall(&b.fallDistance, startY, b.Y, onGround)
	return onGround
}

// spanCells is the first and last cell a side covers, edges nudged so a box
// resting on a face stays in one cell.
func spanCells(from, span float64) (int32, int32) {
	return int32(math.Floor(from + 1e-9)), int32(math.Floor(from + span - 1e-9))
}

// groundFriction is the mean slipperiness of the shapes just under the
// hull's floor; false when there are none, leaving the hull in air drag.
// vanilla: AbstractBoat.getGroundFriction, Block.getFriction
func (b *Boat) groundFriction(w World) (float64, bool) {
	box := b.boatBox()
	box.maxY, box.minY = box.minY, box.minY-0.001
	x0, x1 := int32(math.Floor(box.minX))-1, int32(math.Ceil(box.maxX))+1
	y0, y1 := int32(math.Floor(box.minY))-1, int32(math.Ceil(box.maxY))+1
	z0, z1 := int32(math.Floor(box.minZ))-1, int32(math.Ceil(box.maxZ))+1
	// The sum and division are floats in AbstractBoat.getGroundFriction;
	// rounding once after a double sum changes the drift on mixed surfaces.
	var sum float32
	n := 0
	for x := x0; x < x1; x++ {
		for z := z0; z < z1; z++ {
			edges := 0
			if x == x0 || x == x1-1 {
				edges++
			}
			if z == z0 || z == z1-1 {
				edges++
			}
			if edges == 2 {
				continue
			}
			for y := y0; y < y1; y++ {
				if edges > 0 && (y == y0 || y == y1-1) {
					continue
				}
				state := w.BlockAt(x, y, z)
				if registry.BlockOf(state) == registry.BlockLilyPad {
					continue
				}
				ox, oz := world.CollisionOffset(state, x, z)
				// Empty collision context answers scaffolding's isAbove default (true).
				for _, shape := range cellBoxes(w, noCtx, state, x, y, z, math.Inf(1)) {
					contact := NewAABB(float64(x)+ox+shape.MinX, float64(y)+shape.MinY, float64(z)+oz+shape.MinZ,
						float64(x)+ox+shape.MaxX, float64(y)+shape.MaxY, float64(z)+oz+shape.MaxZ)
					if box.Crosses(contact) {
						sum += float32(rules.BlockFriction(state))
						n++
						break
					}
				}
			}
		}
	}
	if n == 0 {
		return 0, false
	}
	return float64(sum / float32(n)), true
}

// burnTick counts a burn down: the on_fire bite every 20th tick outside
// lava, and the flag follows what is left.
// vanilla: Entity.baseTick
func (b *Boat) burnTick() {
	b.LavaHurts, b.Extinguishes = 0, 0
	b.DousedCauldrons = b.DousedCauldrons[:0]
	if b.FireTicks > 0 {
		if b.FireTicks%20 == 0 && b.lava.height <= 0 {
			b.Hurt(float64(onFireDamage), rules.DamageTypeOnFire, false)
		}
		b.FireTicks--
	}
	b.Burning = b.FireTicks > 0
}

// TakeBounce reports and clears a bounce since the last call: vanilla asks the
// tracker for a position update at once. vanilla: Entity.restituteMovementAfterCollisions
func (b *Boat) TakeBounce() bool {
	n := b.bounced
	b.bounced = false
	return n
}

// FallDistance is the fall banked since the body last stood on the ground.
func (b *Boat) FallDistance() float64 { return b.fallDistance }

// SetFallDistance replaces the banked fall.
func (b *Boat) SetFallDistance(d float64) { b.fallDistance = d }
