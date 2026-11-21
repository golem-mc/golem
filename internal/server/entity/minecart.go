package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The minecart: vanilla's AbstractMinecart for the plain MINECART. Movement is
// OldMinecartBehavior, since the level this server writes leaves
// minecart_improvements off. Step holds the whole rail ride: projection
// onto the lane, slopes, powered-rail boost, and derailing off the end.
// Like the boat a plain value: nothing sent or locked, the server reads
// what the tick leaves.

// Cart numbers: EntityTypes.MINECART's sized(0.98F, 0.7F) box and
// passengerAttachments(0.1875F) seat; AbstractMinecart.getDefaultGravity,
// 0.04 normally and 0.005 in water.
// vanilla: EntityTypes.MINECART, AbstractMinecart.getDefaultGravity
const (
	MinecartHalfWidth = 0.49
	MinecartHeight    = 0.7
	MinecartSeatY     = 0.1875

	// MINECART's clientTrackingRange(8) and the builder default
	// updateInterval(3).
	MinecartTrackingRange  = 8
	MinecartUpdateInterval = 3

	// The rail step cap (0.4, 8 blocks a second; halved in water), the share
	// a rider takes off the step, and the absolute cap off the rails.
	// vanilla: OldMinecartBehavior, AbstractMinecart.comeOffTrack
	MinecartMaxRailSpeed  = 0.4
	MinecartMaxWaterSpeed = 0.2
	MinecartRiderSlow     = 0.75

	minecartGravity     = 0.04
	minecartWaterFactor = 0.005

	// The per-tick rolling pace: a ridden cart keeps 0.997 of its speed, an
	// empty one 0.96.
	// vanilla: OldMinecartBehavior.getSlowdownFactor
	minecartRiddenSlowdown = 0.997
	minecartEmptySlowdown  = 0.96

	// minecartSlideSpeed is a slope's pull, 1/128 a tick, a fifth of it in
	// water. vanilla: OldMinecartBehavior.moveAlongTrack
	minecartSlideSpeed = 0.0078125

	// A lit powered rail's boost to a moving cart, and its shove to a resting
	// one away from a redstone conductor at one end.
	// vanilla: OldMinecartBehavior.moveAlongTrack's power half
	minecartBoost = 0.06
	minecartStart = 0.02

	// minecartHaltSpeed is the speed under which a dark powered rail stops a
	// cart dead; faster carts are only halved.
	minecartHaltSpeed = 0.03

	// VehicleEntity's rocking numbers: ten ticks per blow, a break at 40
	// accumulated, ten times the blow added, and a lit activator's flat 50
	// shake, which rocks past the break mark harmlessly since only blows
	// accumulate.
	MinecartHurtTicks   = 10
	MinecartBreakDamage = 40.0
	MinecartShakeDamage = 50.0
	MinecartBlowFactor  = 10.0
	// MinecartHurtDirDefault is the metadata's default rocking direction, 1.
	MinecartHurtDirDefault = 1

	// Derailed paces: half of every axis lost per ground tick, and the air's
	// 0.95 kept per airborne tick.
	minecartGroundFriction = 0.5
	minecartAirDrag        = 0.95
	// minecartAirDragFloat is AbstractMinecart.getAirDrag's 0.95f, the drag a
	// bounce uses.
	minecartAirDragFloat = float64(float32(0.95))

	// minecartWaterSlowdown is AbstractMinecart.WATER_SLOWDOWN_FACTOR: a
	// twentieth off the rail ride per tick in water.
	minecartWaterSlowdown = 0.95
)

// railExits are each rail shape's two lane ends as offsets from the rail's
// cell (AbstractMinecart.EXITS). An ascending shape's low end is one below
// (Y offset -1), so the lane climbs a block across the cell.
// vanilla: AbstractMinecart.exits
var railExits = map[string][2][3]int32{
	"north_south":     {{0, 0, -1}, {0, 0, 1}},
	"east_west":       {{-1, 0, 0}, {1, 0, 0}},
	"ascending_east":  {{-1, -1, 0}, {1, 0, 0}},
	"ascending_west":  {{-1, 0, 0}, {1, -1, 0}},
	"ascending_north": {{0, 0, -1}, {0, -1, 1}},
	"ascending_south": {{0, -1, -1}, {0, 0, 1}},
	"south_east":      {{0, 0, 1}, {1, 0, 0}},
	"south_west":      {{0, 0, 1}, {-1, 0, 0}},
	"north_west":      {{0, 0, -1}, {-1, 0, 0}},
	"north_east":      {{0, 0, -1}, {1, 0, 0}},
}

// Minecart is one plain minecart in a world. The rocking (ticks left,
// direction, accumulated damage wearing off by one a tick, breaking past
// 40) is its metadata. Flipped is swapped by a near-reverse turn so the
// client's model stays upright. Activated is set on a lit activator; the
// server dismounts the riders and clears it. NoLoot marks an end that drops
// nothing (creative blow, below the world); Dead one the server's next pass
// removes, with its break's drop.
type Minecart struct {
	stride stepState
	// Movement is the vibration produced by the last Step.
	Movement MovementEvent
	waterEntry

	// Splash is the water entry of the last Step, nil for none.
	Splash *WaterSplash
	// bounced marks a landing that sent the cart back up; TakeBounce reads it.
	bounced bool
	// bounceFx is bounced for the tick that moved the cart; TakeBounceEffect reads it.
	bounceFx bool

	ID int32
	Place
	VX, VY, VZ float64
	Yaw        float32
	OnGround   bool
	// fallDistance is the fall banked since the cart last stood on the ground.
	fallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none; the
	// server's pass reads and clears it. vanilla: Entity.checkFallDamage
	LandedFall float64
	// HorizontalCollision is whether the last move met a wall.
	// vanilla: Entity.horizontalCollision
	HorizontalCollision bool
	// FireTicks is ignition state only: AbstractMinecart.tick skips Entity.baseTick.
	FireTicks int
	// LavaHurts and Extinguishes count this tick's sounds.
	LavaHurts, Extinguishes int
	burnHurt                func(float64, registry.DamageType, bool) bool

	// vanilla: Entity.isNoGravity
	NoGravity bool
	// Invulnerable is the permanent Invulnerable save flag.
	Invulnerable bool

	HurtTime   int32
	HurtDir    int32
	HurtDamage float64
	Flipped    bool
	// FirstTick is true until the cart's first tick, written as the saved
	// HasTicked flag. vanilla: Entity.firstTick
	FirstTick bool

	// ShakesOnActivator is true for the rideable cart only.
	// vanilla: Minecart.activateMinecart
	ShakesOnActivator bool
	Activated         bool
	// OnActivator is set for a tick on any activator rail, with
	// ActivatorPowered its state; the server reads and clears it.
	// vanilla: AbstractMinecart.activateMinecart
	OnActivator      bool
	ActivatorPowered bool
	NoLoot           bool
	Dead             bool

	// Slowdown overrides the rolling pace for kinds with their own (container
	// carts); 0 keeps the default.
	Slowdown float64

	// IntentX, IntentZ are the player rider's move intent for this tick (unit
	// length, zero without one); the server sets them before Step.
	// vanilla: ServerPlayer.getLastClientMoveIntent
	IntentX, IntentZ float64

	// Furnace is a furnace cart's fuel and push, nil for every other kind.
	Furnace *FurnaceCart
	// Snowflakes is the Y of each powder snow cell this tick's block checks
	// found the cart moving sideways in, one per check per cell; the server
	// rolls each for a snowflake.
	Snowflakes []int32

	water fluidTracker
	// stuck is the multiplier the cells the cart's last block check found
	// leave for the next move.
	stuck stuckFactor
}

// NewMinecart places a still, unhurt cart at the position. The direction
// count starts at the metadata default of 1.
func NewMinecart(id int32, x, y, z float64) *Minecart {
	return &Minecart{ID: id, X: x, Y: y, Z: z, HurtDir: MinecartHurtDirDefault, FirstTick: true}
}

// maxSpeed is the most a rail step moves the cart along an axis.
// vanilla: AbstractMinecart.getMaxSpeed, MinecartFurnace.getMaxSpeed
func (m *Minecart) maxSpeed(inWater bool) float64 {
	speed := MinecartMaxRailSpeed
	if inWater {
		speed = MinecartMaxWaterSpeed
	}
	if m.Furnace != nil {
		if inWater {
			return speed * furnaceWaterSpeed
		}
		return speed * furnaceLandSpeed
	}
	return speed
}

// gravity is AbstractMinecart.getDefaultGravity: 0.04, 0.005 in water, 0
// when Entity.isNoGravity.
func (m *Minecart) gravity(inWater bool) float64 {
	if m.NoGravity {
		return 0
	}
	if inWater {
		return minecartWaterFactor
	}
	return minecartGravity
}

// Box is the sized(0.98F, 0.7F) box about the position, for blow reach and
// placement collision.
func (m *Minecart) Box() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return m.X - MinecartHalfWidth, m.Y, m.Z - MinecartHalfWidth,
		m.X + MinecartHalfWidth, m.Y + MinecartHeight, m.Z + MinecartHalfWidth
}

// EyePosition is Entity.getEyePosition at the default eye height of 0.85 of
// the cart's height, where an explosion's knockback is directed from.
// vanilla: Entity.getEyePosition, EntityDimensions.eyeHeight
func (m *Minecart) EyePosition() (x, y, z float64) {
	return m.X, m.Y + MinecartHeight*0.85, m.Z
}

// Push is Entity.push for an explosion's knockback.
// vanilla: Entity.push
func (m *Minecart) Push(vx, vy, vz float64) {
	m.VX += vx
	m.VY += vy
	m.VZ += vz
}

// MinecartTarget is the cart as a projectile target: pickable while it
// stands, and the root of its own vehicle group.
// vanilla: AbstractMinecart.isPickable
func MinecartTarget(id, entityType int32, m *Minecart) ArrowTarget {
	minX, minY, minZ, maxX, maxY, maxZ := m.Box()
	return ArrowTarget{
		EntityID: id,
		Type:     entityType,
		MinX:     minX, MinY: minY, MinZ: minZ,
		MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		RootVehicle: id,
	}
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (m *Minecart) CapFall() { CapFallAccumulation(m.VY, &m.fallDistance) }

// BlastBox is Box, the name the other kinds' snapshots read.
func (m *Minecart) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return m.Box()
}

// TakeDamage is VehicleEntity.hurtServer for a cart: rock ten ticks, flip
// the direction, add ten times the damage. A creative blow discards it with
// nothing dropped; a survival blow past 40 breaks it, dropping the cart
// item. It reports whether the blow landed.
// vanilla: VehicleEntity.hurtServer
func (m *Minecart) TakeDamage(amount float64, creative bool) bool {
	return m.Hurt(amount, rules.DamageTypeGeneric, creative)
}

// Hurt applies a typed hurt, including the permanent invulnerability gate.
// vanilla: VehicleEntity.hurtServer, Entity.isInvulnerableToBase
func (m *Minecart) Hurt(amount float64, damageType registry.DamageType, creative bool) bool {
	if m.Dead || (m.Invulnerable && !creative && !rules.InvulnerabilityBypassed(damageType)) {
		return false
	}
	m.HurtDir = -m.HurtDir
	m.HurtTime = MinecartHurtTicks
	m.HurtDamage += amount * MinecartBlowFactor
	if creative {
		m.Dead = true
		m.NoLoot = true
	} else if m.HurtDamage > MinecartBreakDamage {
		m.Dead = true
	}
	return true
}

// StepWithHurt advances a cart whose kind overrides contact damage.
func (m *Minecart) StepWithHurt(w World, ridden bool, hurt func(float64, registry.DamageType, bool) bool) {
	m.burnHurt = hurt
	defer func() { m.burnHurt = nil }()
	m.Step(w, ridden)
}

func (m *Minecart) contactHurt(amount float64, damageType registry.DamageType, creative bool) bool {
	if m.burnHurt != nil {
		return m.burnHurt(amount, damageType, creative)
	}
	return m.Hurt(amount, damageType, creative)
}

// Step advances the cart one tick: rocking runs down, the below-world
// discard, gravity, then the ride (the rail lane if a rail holds it, else
// the ground). The yaw follows the line, turned about when flipped, and a
// near-reverse turn swaps the flip so the client's model stays upright.
// vanilla: AbstractMinecart.tick, OldMinecartBehavior.tick
func (m *Minecart) Step(w World, ridden bool) {
	m.LavaHurts, m.Extinguishes = 0, 0
	m.Snowflakes = m.Snowflakes[:0]
	m.Splash = nil
	m.Movement = MovementEvent{}
	if m.Dead {
		return
	}
	m.FirstTick = false
	if m.HurtTime > 0 {
		m.HurtTime--
	}
	if m.HurtDamage > 0 {
		m.HurtDamage--
	}
	// The base tick's below-world check: 64 below the floor it is discarded,
	// dropping nothing.
	if m.Y < float64(w.MinY())-64 {
		m.Dead = true
		m.NoLoot = true
		return
	}
	// The behavior reads the previous tick's fluid state, initially dry.
	// vanilla: AbstractMinecart.tick
	inWater := m.water.height > 0

	oldYaw := m.Yaw
	oldX, oldZ := m.X, m.Z
	m.VY -= m.gravity(inWater)

	posX, posY, posZ, state := m.railCell(w)
	if shape := railShapeOf(state); shape != "" {
		m.moveAlongTrack(w, posX, posY, posZ, state, shape, inWater, ridden)
		if registry.BlockOf(state) == registry.BlockActivatorRail {
			m.OnActivator = true
			m.ActivatorPowered = registry.StatePowered(state)
		}
		// vanilla: Minecart.activateMinecart
		if m.ShakesOnActivator && m.OnActivator && m.ActivatorPowered {
			m.Activated = true
			if m.HurtTime == 0 {
				m.HurtDir = -m.HurtDir
				m.HurtTime = MinecartHurtTicks
				m.HurtDamage = MinecartShakeDamage
			}
		}
	} else {
		m.comeOffTrack(w, inWater)
	}

	m.VY, _ = bubbleColumnDrift(w, m.cartBox(), m.VY)
	m.blockEffects(w)
	m.Splash = m.step(w, m.cartBox(), m.VX, m.VY, m.VZ)
	var lava fluidTracker
	updateFluid(w, m.cartBox(), m.X, m.Y+MinecartHeight, m.Z, false, &m.water, &lava)
	// The explicit lava hurt still succeeds after block effects removed the cart.
	// vanilla: AbstractMinecart.tick, VehicleEntity.hurtServer
	if BoxInLava(w, m.X, m.Y, m.Z, MinecartHalfWidth*2, MinecartHeight) {
		m.FireTicks = max(m.FireTicks, fireTicksLava)
		if m.Dead || m.contactHurt(float64(lavaDamage), rules.DamageTypeLava, false) {
			m.LavaHurts++
		}
		m.fallDistance *= 0.5
	}

	xDiff, zDiff := oldX-m.X, oldZ-m.Z
	if xDiff*xDiff+zDiff*zDiff > 0.001 {
		m.Yaw = float32(math.Atan2(zDiff, xDiff) * 180 / math.Pi)
		if m.Flipped {
			m.Yaw += 180
		}
	}
	// Wrap the turn to [-180, 180) first so a full turn does not read as a
	// near-reverse. vanilla: Mth.wrapDegrees
	if rotDiff := math.Mod(float64(m.Yaw-oldYaw)+540, 360) - 180; rotDiff < -170 || rotDiff >= 170 {
		m.Yaw += 180
		m.Flipped = !m.Flipped
	}
	m.Yaw = float32(math.Mod(float64(m.Yaw), 360))
}

// blockEffects is one of the tick's two block checks at the cart's place,
// after the move and at the tick's end: the stuck multiplier a cell leaves
// for the next move, and the powder snow cells a snowflake may rise above.
// The clamp getKnownMovement puts on the cart's velocity keeps its zeroes.
// vanilla: AbstractMinecart.move, AbstractMinecart.applyEffectsFromBlocks,
// OldMinecartBehavior.getKnownMovement
func (m *Minecart) blockEffects(w World) {
	sideways := m.VX != 0 || m.VZ != 0
	move := bodyMove{fx: m.X, fy: m.Y, fz: m.Z, tx: m.X, ty: m.Y, tz: m.Z}
	rel := AABB{-MinecartHalfWidth, 0, -MinecartHalfWidth, MinecartHalfWidth, MinecartHeight, MinecartHalfWidth}
	burns, fizzles := vehicleBurnPass(w, move, rel, &m.FireTicks, &m.Dead, m.contactHurt, func(x, y, z int32, state uint16, _ int, _ AABB, _, _, _ float64) bool {
		if insideNonLiving(state, sideways, &m.stuck) {
			m.Snowflakes = append(m.Snowflakes, y)
		}
		return true
	})
	m.LavaHurts += burns
	m.Extinguishes += fizzles
}

// cartBox is the cart's span as a fluid box.
func (m *Minecart) cartBox() AABB {
	return AABB{m.X - MinecartHalfWidth, m.Y, m.Z - MinecartHalfWidth,
		m.X + MinecartHalfWidth, m.Y + MinecartHeight, m.Z + MinecartHalfWidth}
}

// railCell finds the cell the ride reads: the cart's floor cell, or the one
// below when it holds a rail, with its state.
// vanilla: AbstractMinecart.getCurrentBlockPosOrRailBelow
func (m *Minecart) railCell(w World) (x, y, z int32, state uint16) {
	x, y, z = int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))
	if below := w.BlockAt(x, y-1, z); railShapeOf(below) != "" {
		return x, y - 1, z, below
	}
	return x, y, z, w.BlockAt(x, y, z)
}

// railShapeOf reads a rail state's shape property, "" for a non-rail. The
// four rails share the minecraft:rails tag that isRail reads.
func railShapeOf(state uint16) string {
	if !registry.BlockOf(state).InTag(railBlocks) {
		return ""
	}
	return registry.StateValue(state, registry.PropShape)
}

// moveAlongTrack runs one tick on the rail at (posX, posY, posZ): a slope's
// slide, the velocity projected onto the lane, the position pinned to it,
// the step, ground slowdown, the slope climb, then the powered rail (boost
// when moving, a shove off a conductor when resting, a halt on a dark one).
// vanilla: OldMinecartBehavior.moveAlongTrack
func (m *Minecart) moveAlongTrack(w World, posX, posY, posZ int32, state uint16, shape string, inWater, ridden bool) {
	m.fallDistance = 0
	x, y, z := m.X, m.Y, m.Z
	oldPos, oldOK := m.railPoint(w, x, y, z)
	// Mid-tick height: the rail cell's floor, a block higher when ascending.
	y = float64(posY)

	// A powered rail's POWERED, and whether a dark one halts.
	powerTrack, haltTrack := false, false
	poweredRail := registry.BlockOf(state) == registry.BlockPoweredRail
	if poweredRail && registry.StatePowered(state) {
		powerTrack = true
	} else if poweredRail {
		haltTrack = true
	}

	slide := minecartSlideSpeed
	if inWater {
		slide *= 0.2
	}
	switch shape {
	case "ascending_east":
		m.VX -= slide
		y++
	case "ascending_west":
		m.VX += slide
		y++
	case "ascending_north":
		m.VZ += slide
		y++
	case "ascending_south":
		m.VZ -= slide
		y++
	}

	// Project the velocity onto the lane: only speed along the rail is kept,
	// bent by a curve and straightened or reversed at a slope's end.
	exits := railExits[shape]
	xD := float64(exits[1][0] - exits[0][0])
	zD := float64(exits[1][2] - exits[0][2])
	length := math.Sqrt(xD*xD + zD*zD)
	if m.VX*xD+m.VZ*zD < 0 {
		xD, zD = -xD, -zD
	}
	pow := math.Min(2, math.Hypot(m.VX, m.VZ))
	m.VX, m.VZ = pow*xD/length, pow*zD/length

	// A player rider's keys nudge a near-still cart along their heading, and
	// the nudge overrides a dark powered rail's halt for the tick.
	if (m.IntentX != 0 || m.IntentZ != 0) && m.VX*m.VX+m.VZ*m.VZ < 0.01 {
		m.VX += m.IntentX * 0.001
		m.VZ += m.IntentZ * 0.001
		haltTrack = false
	}
	// A dark powered rail halves any cart and stops a slow one.
	if haltTrack {
		if speed := math.Hypot(m.VX, m.VZ); speed < minecartHaltSpeed {
			m.VX, m.VZ = 0, 0
		} else {
			m.VX *= 0.5
			m.VZ *= 0.5
		}
	}

	// Pin the cart onto the lane: its share along the exit-to-exit line, by
	// either axis for a straight lane, by projection for a curve.
	x0 := float64(posX) + 0.5 + float64(exits[0][0])*0.5
	z0 := float64(posZ) + 0.5 + float64(exits[0][2])*0.5
	x1 := float64(posX) + 0.5 + float64(exits[1][0])*0.5
	z1 := float64(posZ) + 0.5 + float64(exits[1][2])*0.5
	spanX, spanZ := x1-x0, z1-z0
	progress := 0.0
	switch {
	case spanX == 0:
		progress = z - float64(posZ)
	case spanZ == 0:
		progress = x - float64(posX)
	default:
		progress = ((x-x0)*spanX + (z-z0)*spanZ) * 2
	}
	m.SetPos(x0+spanX*progress, y, z0+spanZ*progress)

	// The step: a rider takes a quarter off, capped for water or land. A
	// wall stops its axis.
	scale := 1.0
	if ridden {
		scale = MinecartRiderSlow
	}
	maxSpeed := m.maxSpeed(inWater)
	dx, _, dz, held := m.stuck.scale(clampStep(m.VX*scale, maxSpeed), 0, clampStep(m.VZ*scale, maxSpeed))
	if held {
		m.VX, m.VY, m.VZ = 0, 0, 0
	}
	fromX, fromZ := m.X, m.Z
	box, _, xBlocked, zBlocked := collideHorizontal(w, fallCtx(m.fallDistance), m.cartBox(), dx, dz)
	m.SetPos((box.minX+box.maxX)/2, m.Y, (box.minZ+box.maxZ)/2)
	// Emit before the lane projection changes Y and the supporting block.
	m.Movement = m.stride.vehicleEvent(w, bodyMove{fx: fromX, fy: m.Y, fz: fromZ, tx: m.X, ty: m.Y, tz: m.Z}, MinecartHalfWidth, false, true, inWater)
	m.HorizontalCollision = xBlocked || zBlocked
	if xBlocked {
		m.VX = 0
	}
	if zBlocked {
		m.VZ = 0
	}
	m.blockEffects(w)
	// Crossing into the neighbour a slope's exit climbs or drops to steps the
	// cart with it. A riding cart stays off the ground: the vertical step was
	// flat, so no floor was met.
	floorX := int32(math.Floor(m.X)) - posX
	floorZ := int32(math.Floor(m.Z)) - posZ
	m.OnGround = false
	if exits[0][1] != 0 && floorX == exits[0][0] && floorZ == exits[0][2] {
		m.Y += float64(exits[0][1])
	} else if exits[1][1] != 0 && floorX == exits[1][0] && floorZ == exits[1][2] {
		m.Y += float64(exits[1][1])
	}

	// Rolling pace: a ridden cart keeps nearly all its speed, an empty one
	// loses 1/25, water another 1/20 off both. The vertical motion is zeroed:
	// the rail holds the cart, so this tick's gravity never accumulates.
	// vanilla: AbstractMinecart.applyNaturalSlowdown
	slowdown := minecartEmptySlowdown
	if ridden {
		slowdown = minecartRiddenSlowdown
	}
	if m.Slowdown != 0 {
		slowdown = m.Slowdown
	}
	if inWater {
		slowdown *= minecartWaterSlowdown
	}
	if m.Furnace != nil {
		// The push is turned along this tick's gravity as the vertical motion;
		// only the horizontal result is kept.
		m.VX, m.VZ = m.Furnace.Slowdown(m.VX, -m.gravity(inWater), m.VZ, inWater)
	}
	m.VX *= slowdown
	m.VY = 0
	m.VZ *= slowdown

	// Back onto the lane for the climb: the height projected from both ends,
	// with the step's drop feeding the speed, so a downhill run regains its
	// drop and an uphill climb pays its rise.
	newPos, newOK := m.railPoint(w, m.X, m.Y, m.Z)
	if newOK && oldOK {
		speed := (oldPos[1] - newPos[1]) * 0.05
		if horizontal := math.Hypot(m.VX, m.VZ); horizontal > 0 {
			m.VX *= (horizontal + speed) / horizontal
			m.VZ *= (horizontal + speed) / horizontal
		}
		m.Y = newPos[1]
	}

	// Crossing into a new cell carries the whole speed, turned to that cell's
	// direction.
	floorX, floorZ = int32(math.Floor(m.X))-posX, int32(math.Floor(m.Z))-posZ
	if floorX != 0 || floorZ != 0 {
		speed := math.Hypot(m.VX, m.VZ)
		m.VX = speed * float64(floorX)
		m.VZ = speed * float64(floorZ)
	}

	if powerTrack {
		if speed := math.Hypot(m.VX, m.VZ); speed > 0.01 {
			m.VX += m.VX / speed * minecartBoost
			m.VZ += m.VZ / speed * minecartBoost
		} else {
			// A resting cart is shoved off the lit rail's end away from a redstone
			// conductor.
			dx, dz := m.VX, m.VZ
			toWest := conductorAt(w, posX-1, posY, posZ)
			toEast := conductorAt(w, posX+1, posY, posZ)
			toNorth := conductorAt(w, posX, posY, posZ-1)
			toSouth := conductorAt(w, posX, posY, posZ+1)
			switch shape {
			case "east_west":
				if toWest {
					dx = minecartStart
				} else if toEast {
					dx = -minecartStart
				}
			case "north_south":
				if toNorth {
					dz = minecartStart
				} else if toSouth {
					dz = -minecartStart
				}
			default:
				return
			}
			m.VX, m.VZ = dx, dz
		}
	}
}

// conductorAt reports a redstone conductor at the cell, which a lit powered
// rail shoves a resting cart away from. A world that knows block entities
// answers for a shulker box's lid; a stub falls back to the block name.
// vanilla: AbstractMinecart.isRedstoneConductor
func conductorAt(w World, x, y, z int32) bool {
	if c, ok := w.(interface{ ConductorAt(x, y, z int32) bool }); ok {
		return c.ConductorAt(x, y, z)
	}
	return registry.IsRedstoneConductorState(w.BlockAt(x, y, z))
}

// clampStep caps one axis of a rail step at the water or land limit.
func clampStep(v, max float64) float64 {
	return math.Max(-max, math.Min(max, v))
}

// comeOffTrack runs a tick off the rails: cap the speed, halve everything
// if the last step ended on the ground, move, and apply air drag if the
// tick ends airborne.
// vanilla: AbstractMinecart.comeOffTrack
func (m *Minecart) comeOffTrack(w World, inWater bool) {
	maxSpeed := m.maxSpeed(inWater)
	m.VX = clampStep(m.VX, maxSpeed)
	m.VZ = clampStep(m.VZ, maxSpeed)
	if m.OnGround {
		m.VX *= minecartGroundFriction
		m.VY *= minecartGroundFriction
		m.VZ *= minecartGroundFriction
	}
	// The move, falling axis first (a derailed step is mostly fall); walls and
	// floors zero their axis, and a stopped fall grounds the cart. Horizontal
	// axes go larger first, as in collideHorizontal.
	ctx, startY := fallCtx(m.fallDistance), m.Y
	fromX, fromZ := m.X, m.Z
	// vanilla: Entity.move spends the stuck multiplier and the velocity.
	dx, dy, dz, held := m.stuck.scale(m.VX, m.VY, m.VZ)
	if held {
		m.VX, m.VY, m.VZ = 0, 0, 0
	}
	box, _, yBlocked := clipMove(w, ctx, m.cartBox(), axisY, dy)
	box, _, xBlocked, zBlocked := collideHorizontal(w, ctx, box, dx, dz)
	m.SetPos((box.minX+box.maxX)/2, box.minY, (box.minZ+box.maxZ)/2)
	m.OnGround = yBlocked && dy < 0
	m.Movement = m.stride.vehicleEvent(w, bodyMove{fx: fromX, fy: startY, fz: fromZ, tx: m.X, ty: m.Y, tz: m.Z}, MinecartHalfWidth, m.OnGround, false, inWater)
	m.HorizontalCollision = xBlocked || zBlocked
	if m.OnGround && m.water.height <= 0 {
		if fall := m.fallDistance + math.Max(startY-m.Y, 0); fall > 0 {
			m.LandedFall = fall
		}
	}
	bankFall(&m.fallDistance, startY, m.Y, m.OnGround)
	if yBlocked {
		if dy < 0 {
			m.VY = bounceOffGround(w, m.X, m.Y, m.Z, MinecartHalfWidth, m.VY, m.Y-startY, m.gravity(inWater), minecartAirDragFloat, false)
			if m.VY > 0 {
				m.bounced, m.bounceFx = true, true
			}
		} else {
			m.VY = 0
		}
	}
	if xBlocked {
		m.VX = 0
	}
	if zBlocked {
		m.VZ = 0
	}
	// The move's block effects run before the drag; Step runs them again
	// after the tick.
	// vanilla: Entity.move, OldMinecartBehavior.tick
	m.VY, _ = bubbleColumnDrift(w, m.cartBox(), m.VY)
	m.blockEffects(w)
	if !m.OnGround {
		m.VX *= minecartAirDrag
		m.VY *= minecartAirDrag
		m.VZ *= minecartAirDrag
	}
}

// railPoint projects a point onto the lane of the rail in its cell or the
// one below: the projection moveAlongTrack pins to and measures slopes by.
// The lane runs between the exits' outer edges, 1/16 above the rail's floor,
// a slope's low end lifted by half (the climb then adds the whole). false
// when no rail holds the point.
// vanilla: OldMinecartBehavior.getPos
func (m *Minecart) railPoint(w World, x, y, z float64) ([3]float64, bool) {
	xt, yt, zt := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	state := w.BlockAt(xt, yt-1, zt)
	shape := railShapeOf(state)
	if shape != "" {
		yt--
	} else {
		state = w.BlockAt(xt, yt, zt)
		shape = railShapeOf(state)
		if shape == "" {
			return [3]float64{}, false
		}
	}
	exits := railExits[shape]
	x0 := float64(xt) + 0.5 + float64(exits[0][0])*0.5
	y0 := float64(yt) + 0.0625 + float64(exits[0][1])*0.5
	z0 := float64(zt) + 0.5 + float64(exits[0][2])*0.5
	x1 := float64(xt) + 0.5 + float64(exits[1][0])*0.5
	y1 := float64(yt) + 0.0625 + float64(exits[1][1])*0.5
	z1 := float64(zt) + 0.5 + float64(exits[1][2])*0.5
	spanX, spanZ := x1-x0, z1-z0
	spanY := (y1 - y0) * 2
	progress := 0.0
	switch {
	case spanX == 0:
		progress = z - float64(zt)
	case spanZ == 0:
		progress = x - float64(xt)
	default:
		progress = ((x-x0)*spanX + (z-z0)*spanZ) * 2
	}
	x = x0 + spanX*progress
	y = y0 + spanY*progress
	z = z0 + spanZ*progress
	if spanY < 0 {
		y++
	} else if spanY > 0 {
		y += 0.5
	}
	return [3]float64{x, y, z}, true
}

// directionIndex is getDirection's quarter from the yaw: south 0, then
// west, north, east.
func (m *Minecart) directionIndex() int32 {
	return int32(math.Floor(float64(m.Yaw)*4/360+0.5)) & 3
}

// MotionDir is getMotionDirection, the dismount's direction: the yaw's
// direction turned a quarter clockwise (south to west, in HORIZONTALS
// order), reversed when flipped.
// vanilla: OldMinecartBehavior.getMotionDirection, Direction.getClockWise
func (m *Minecart) MotionDir() (dx, dz int32) {
	// HORIZONTALS order: south, west, north, east.
	steps := [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}}
	d := m.directionIndex()
	if m.Flipped {
		d = (d + 2) & 3
	}
	d = (d + 1) & 3
	return steps[d][0], steps[d][1]
}

// Crosses reports whether the cart's box shares an inside with box, for
// detector rails.
func (m *Minecart) Crosses(box AABB) bool {
	return NewAABB(m.Box()).Crosses(box)
}

// PickupBox is the box a rideable cart takes a body aboard from, the cart's
// own grown 0.2 (a float) sideways; false while it moves under 0.1 blocks a
// tick, too slow to pick anyone up.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func (m *Minecart) PickupBox() (AABB, bool) {
	if !m.Moving() {
		return AABB{}, false
	}
	return m.CartPushBox(), true
}

// Moving reports whether the cart moves 0.1 blocks a tick or more
// sideways, fast enough to pick up or shove a body.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func (m *Minecart) Moving() bool {
	return m.VX*m.VX+m.VZ*m.VZ >= 0.01
}

// MinecartBoardable reports whether a moving cart may pick the mob up: a
// pushable body that may ride and has not just left a cart, an iron golem
// or a wither never.
// vanilla: OldMinecartBehavior.pushAndPickupEntities, EntitySelector.pushableBy, Entity.canRide, WitherBoss.canRide
func MinecartBoardable(m *Mob) bool {
	return m.Pushable() && m.Type != TypeIronGolem && m.Type != TypeWither && m.CanRide() && m.BoardingCooldown <= 0
}

// Cart push numbers. vanilla: AbstractMinecart.push, pushOtherMinecart
const (
	cartPushDeadband = float64(float32(1e-4))
	// cartPushScale is applied as two factors, 0.1f then 0.5, for vanilla's rounding.
	cartPushScale = float64(float32(0.1))
	// cartFacingDot is the least |cos| between the cart's facing and the line
	// to the other cart for the two to trade motion.
	cartFacingDot = float64(float32(0.8))
	// vecNormalizeMin is the length under which Vec3.normalize gives zero.
	vecNormalizeMin = float64(float32(1e-5))
)

// PushFrom is AbstractMinecart.push from a body at (x, z): the cart takes
// the push away from it and the result is the body's quarter share. When
// other is the cart at (x, z) the two trade motion instead and the share is
// zero.
// vanilla: AbstractMinecart.push
func (m *Minecart) PushFrom(x, z float64, other *Minecart) (bx, bz float64) {
	xa, za := x-m.X, z-m.Z
	dd := xa*xa + za*za
	if dd < cartPushDeadband {
		return 0, 0
	}
	dd = math.Sqrt(dd)
	xa /= dd
	za /= dd
	pow := min(1/dd, 1)
	xa *= pow
	za *= pow
	xa *= cartPushScale
	za *= cartPushScale
	xa *= 0.5
	za *= 0.5
	if other != nil {
		m.pushOtherMinecart(other, xa, za)
		return 0, 0
	}
	m.VX += -xa
	m.VZ += -za
	return xa / 4, za / 4
}

// pushOtherMinecart trades motion with the cart other, (xa, za) pointing
// from this cart to it: only along this cart's line, a furnace cart keeping
// most of its own and the other cart taking it, two alike averaging.
// vanilla: AbstractMinecart.pushOtherMinecart
func (m *Minecart) pushOtherMinecart(other *Minecart, xa, za float64) {
	dirX, dirZ := normalizeXZ(other.X-m.X, other.Z-m.Z)
	rad := float64(m.Yaw * (float32(math.Pi) / 180))
	faceX, faceZ := normalizeXZ(float64(world.MthCos(rad)), float64(world.MthSin(rad)))
	if math.Abs(dirX*faceX+dirZ*faceZ) < cartFacingDot {
		return
	}
	mx, mz := m.VX, m.VZ
	ox, oz := other.VX, other.VZ
	switch {
	case other.Furnace != nil && m.Furnace == nil:
		m.VX, m.VZ = mx*0.2+(ox-xa), mz*0.2+(oz-za)
		other.VX, other.VZ = ox*0.95, oz*0.95
	case other.Furnace == nil && m.Furnace != nil:
		other.VX, other.VZ = ox*0.2+(mx+xa), oz*0.2+(mz+za)
		m.VX, m.VZ = mx*0.95, mz*0.95
	default:
		xdd, zdd := (ox+mx)/2, (oz+mz)/2
		m.VX, m.VZ = mx*0.2+(xdd-xa), mz*0.2+(zdd-za)
		other.VX, other.VZ = ox*0.2+(xdd+xa), oz*0.2+(zdd+za)
	}
}

// normalizeXZ is Vec3.normalize of a level vector.
func normalizeXZ(x, z float64) (float64, float64) {
	d := math.Sqrt(x*x + z*z)
	if d < vecNormalizeMin {
		return 0, 0
	}
	return x / d, z / d
}

// ShovedBy is Entity.push between the moving cart and a body at (x, z) its
// sweep meets: the result is the body's share, and the cart takes the
// opposite when free is set (it carries nobody).
// vanilla: OldMinecartBehavior.pushAndPickupEntities, Entity.push
func (m *Minecart) ShovedBy(x, z float64, free bool) (bx, bz float64) {
	bx, bz = PushApart(m.X, m.Z, x, z)
	if free {
		m.VX -= bx
		m.VZ -= bz
	}
	return bx, bz
}

// CartPushBox is the box the cart pushes or picks up bodies in: its own
// grown 0.2 (a float) sideways.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func (m *Minecart) CartPushBox() AABB {
	const grow = float64(float32(0.2))
	return NewAABB(m.Box()).inflate(grow, 0, grow)
}

// PushesCarts reports whether the mob's own push step runs and so pushes the
// carts it overlaps: a seated body is pushed via its mount, and a vex has no
// physics.
// vanilla: LivingEntity.pushEntities, AbstractMinecart.push
func (m *Mob) PushesCarts() bool {
	return !m.Dead && !m.gone && !m.RidesVehicle && m.Type != TypeVex
}

// TakeBounce reports and clears a bounce since the last call: vanilla asks the
// tracker for a position update at once. vanilla: Entity.restituteMovementAfterCollisions
func (m *Minecart) TakeBounce() bool {
	n := m.bounced
	m.bounced = false
	return n
}

// FallDistance is the fall banked since the body last stood on the ground.
func (b *Minecart) FallDistance() float64 { return b.fallDistance }

// SetFallDistance replaces the banked fall.
func (b *Minecart) SetFallDistance(d float64) { b.fallDistance = d }

// TakeBounceEffect reports and clears a bounce since the last call, apart
// from TakeBounce's tracker signal.
// vanilla: Entity.restituteMovementAfterCollisions
func (m *Minecart) TakeBounceEffect() bool {
	n := m.bounceFx
	m.bounceFx = false
	return n
}
