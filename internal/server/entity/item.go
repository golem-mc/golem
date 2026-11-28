package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Item entity numbers: gravity then air drag per tick until it rests; a
// five-minute life at 20 ticks a second; a block drop is pickable almost at
// once, a thrown or death-scattered item after two seconds. The drag is
// Entity.getAirDrag's Java float widened at each use, hence the float32
// round-trip. Fluid numbers are in fluids.go.
const (
	gravity      = 0.04
	drag         = airDragFloat
	despawnTicks = 6000

	// BreakPickupDelay and ThrownPickupDelay are the spawn pickup delays: a
	// block drop's 10 ticks, and a throw's or death scatter's two seconds.
	BreakPickupDelay  = 10
	ThrownPickupDelay = 40

	// size is the item box's edge, a quarter block. Item coordinates are the
	// box's centre, so a resting item sits half that above its block.
	size     = 0.25
	halfSize = size / 2

	// ItemHalfSize is half the box edge, exported for converting between the
	// simulation's centre and the feet position entity files save.
	ItemHalfSize = halfSize

	// ItemUnlimitedAge is Short.MIN_VALUE, an age the tick never increments,
	// so the item never despawns.
	// vanilla: ItemEntity.tick
	ItemUnlimitedAge = math.MinInt16

	// ItemExtendedAge is the age of an item given a second lifetime.
	// vanilla: ItemEntity.setExtendedLifetime
	ItemExtendedAge = -despawnTicks

	// mergeInflate is the growth the merge scan's search box takes, the
	// AABB.inflate(0.5, 0.0, 0.5) the vanilla scan queries.
	mergeInflate = 0.5
	// ItemMergeReach is the largest X or Z distance between two items'
	// centres at which their merge boxes can meet.
	ItemMergeReach = 2*halfSize + mergeInflate
)

// A player's standing body: 0.6 wide and 1.8 tall, centred on X and Z and
// rising from the reported Y (the feet).
const (
	PlayerWidth  = 0.6
	PlayerHalf   = PlayerWidth / 2
	PlayerHeight = 1.8
)

// ItemEventKind is one thing an item's tick produced that the server may
// want to tell the world about or act on.
type ItemEventKind uint8

const (
	// ItemDestroyed: fire or lava burned the stack up, vanilla's
	// hurtServer discard.
	ItemDestroyed ItemEventKind = iota
	// ItemBurned: a burn or drowning hurt landed and lavaHurt plays its sound
	// this tick: always on the killing hurt, one in ten otherwise.
	ItemBurned
	// ItemExtinguished: a burn put out by water or rain, and vanilla
	// plays the extinguish sound for it.
	ItemExtinguished
	// ItemDousedCauldron: a burning item was extinguished in the layered
	// cauldron at X, Y, Z, which the server lowers a level.
	// vanilla: LayeredCauldronBlock.entityInside
	ItemDousedCauldron
	// ItemSteppedOnRedstoneOre: the item rests on the unlit redstone ore at
	// X, Y, Z, which the server lights.
	// vanilla: RedStoneOreBlock.stepOn
	ItemSteppedOnRedstoneOre
	// ItemBubbleSurface: the item rode a bubble column's surface, where the
	// splash rises.
	// vanilla: Entity.sendBubbleColumnParticles
	ItemBubbleSurface
	// ItemSnowflake: the item moved sideways in the powder snow cell at Y,
	// and may kick up a snowflake above it.
	// vanilla: PowderSnowBlock.entityInside
	ItemSnowflake
	// ItemSplashed: the item first touched water; the server plays
	// entity.generic.splash at Volume and Pitch and posts a splash game event.
	// vanilla: Entity.doWaterSplashEffect
	ItemSplashed
	// ItemBounced: the item bounced off the block below; the server posts a
	// bounce game event.
	// vanilla: Entity.restituteMovementAfterCollisions
	ItemBounced
	// ItemHurt: a hurt landed, the entity_damage game event at the item's
	// feet, posted even when the hurt destroys the stack.
	// vanilla: ItemEntity.hurtServer
	ItemHurt
)

// ItemEvent is one event out of an item's tick.
type ItemEvent struct {
	Kind ItemEventKind
	// X, Y, Z is the column cell of an ItemBubbleSurface, the cauldron of
	// an ItemDousedCauldron, the ore of an ItemSteppedOnRedstoneOre, and Y the powder snow cell of an ItemSnowflake.
	X, Y, Z int32
	// Volume and Pitch are an ItemSplashed's sound.
	Volume, Pitch float32
}

// Item is one item stack lying in a world. The server keeps its dimension,
// what each client knows, and what pickup does; the world is passed in each
// tick.
//
// X, Y, Z is the quarter-block box's centre. A resting item keeps zero
// velocity and falls again if its block goes. Age counts to five minutes,
// PickupDelay to when a player may take it. Dead marks one picked up or
// burned. Health is the five points fire and lava spend.
type Item struct {
	ID    int32
	Stack protocol.ItemStack
	// HurtAsk, when set for the length of a Step, hears each hurt that lands,
	// with the health it was about to spend; the caller may undo it later.
	HurtAsk func(damage float32, damageType registry.DamageType, health int)

	Place
	VX, VY, VZ float64
	OnGround   bool
	// fallDistance is the fall banked since the item last stood on the ground.
	fallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none; the
	// server posts the hit-ground game event for it and clears it.
	LandedFall float64
	// vanilla: Entity.isNoGravity
	NoGravity bool
	// Invulnerable is the permanent Invulnerable save flag.
	Invulnerable bool

	// webbed is the multiplier the cell the item ended its last move in leaves
	// for the next one.
	webbed stuckFactor
	// lastMove is the move whose cells the block effects walk: the last tick
	// that moved, which a resting tick walks again (none before the first).
	// vanilla: Entity.finalMovementsThisTick
	lastMove    bodyMove
	hasLastMove bool

	Age         int
	PickupDelay int
	Health      int
	Dead        bool
	// InvulnerableTime is the temporary invulnerability countdown.
	// vanilla: Entity.invulnerableTime, Entity.isTemporarilyInvulnerable
	InvulnerableTime int

	// Target is the only player who may pick the item up, nil for anyone. The
	// zero UUID is a real target no player has, so such an item is nobody's.
	// Items merge only when their targets agree, so a reserved leftover does
	// not join a public drop.
	// vanilla: ItemEntity.setTarget
	Target *uuid.UUID

	// water and lava are the item's EntityFluidInteraction, refilled twice a
	// tick, before and after the move.
	water, lava fluidTracker

	// fireTicks is remainingFireTicks: lit by lava and fire, biting one damage
	// every 20th tick while it lasts.
	fireTicks int
	// fireFlag is shared flag bit 0 as the base tick last set it: burning
	// after the run-down, before the move can light or douse the item.
	// vanilla: Entity.baseTick, setSharedFlagOnFire
	fireFlag bool

	// tickCount is vanilla's Entity.tickCount, counted from one, whose
	// residue against four decides when a resting item stirs.
	tickCount int
	// wasInWater is Entity.wasTouchingWater.
	wasInWater bool
	// bouncedOff is the last move's landing bounce.
	bouncedOff bool

	// syncNeeded is needsSync: the item is worth a packet ahead of the movement
	// schedule, when in a fluid or when its velocity changed by lengthSqr() >
	// 0.01 (more than 0.1).
	syncNeeded bool

	// mergeTaken marks a stack a merge grew, for the server: the count changed
	// under the clients, who learn it from metadata. The merge runs wherever the
	// scan found the neighbour, so the mark is read off the survivor after the
	// tick's pass.
	mergeTaken bool
	// MergeAsk, when set, is told each merge before it happens and reports
	// whether it took the decision over; the merge then waits for MergeStacks.
	MergeAsk func(survivor, victim *Item) bool

	// Rand is the item's own random source, vanilla's Entity.random: the
	// push out of a buried block asks it for the escape's speed. Nil until
	// the first roll, which seeds it from seed: a source is 5 KB, and most
	// items never roll.
	Rand *rand.Rand
	// seed is what the first roll seeds Rand from, when seeded; an item
	// built without NewItem seeds from its ID.
	seed   int64
	seeded bool
}

// TakeSync reports and clears syncNeeded, as the vanilla server consumes
// it.
func (it *Item) TakeSync() bool {
	if it.syncNeeded {
		it.syncNeeded = false
		return true
	}
	return false
}

// TakeMerge reports and clears mergeTaken (from this item's scan or a
// neighbour's). The new count owes watchers a metadata update; the pickup
// delay and age a merge settles are not sent.
func (it *Item) TakeMerge() bool {
	if it.mergeTaken {
		it.mergeTaken = false
		return true
	}
	return false
}

// NewItem places an item stack at the position with the velocity, five
// health, and its own random source from seed, made on its first roll.
func NewItem(id int32, stack protocol.ItemStack, x, y, z, vx, vy, vz float64, pickupDelay int, seed int64) *Item {
	return &Item{
		ID:          id,
		Stack:       stack,
		X:           x,
		Y:           y,
		Z:           z,
		VX:          vx,
		VY:          vy,
		VZ:          vz,
		Health:      5,
		PickupDelay: pickupDelay,
		seed:        seed,
		seeded:      true,
	}
}

// MakeFake marks the item a full /give throws for the pickup animation:
// never takeable, one tick from despawning.
// vanilla: ItemEntity.makeFakeItem
func (it *Item) MakeFake() {
	it.PickupDelay = pickupDelayInfinite
	it.Age = despawnTicks - 1
}

// dice is the item's own random source, made on the first roll from the
// constructor's seed, or from its ID for an item built without one.
func (it *Item) dice() *rand.Rand {
	if it.Rand == nil {
		seed := int64(it.ID)
		if it.seeded {
			seed = it.seed
		}
		it.Rand = NewDice(seed)
	}
	return it.Rand
}

// fireImmune is ItemEntity.fireImmune: the stack's damage_resistant
// names in_fire (the netherite family's is_fire).
func (it *Item) fireImmune() bool {
	return registry.StackResistsDamage(it.Stack, rules.DamageTypeInFire)
}

// SetFire sets the burn the item still has to run, for a test that lights
// an item without a fire block.
func (it *Item) SetFire(ticks int) { it.fireTicks = ticks }

// FireTicks is the burn the item still has to run.
func (it *Item) FireTicks() int { return it.fireTicks }

// FireFlag is the on-fire bit of the shared flags the clients are synched
// to; unlike isOnFire it ignores fire immunity.
func (it *Item) FireFlag() bool { return it.fireFlag }

// isOnFire is Entity.isOnFire for the item: burning, and burnable — a
// fireproof stack never counts as on fire, whatever the ticks say.
func (it *Item) isOnFire() bool {
	return it.fireTicks > 0 && !it.fireImmune()
}

// hurt is ItemEntity.hurtServer for a blow of damageType, gated by fire
// immunity against an is_fire type and the stack's resistance to the type:
// health takes the damage and an emptied item is destroyed (the burn-up
// event). It reports whether the hurt landed.
// vanilla: Entity.isInvulnerableToBase, ItemStack.canBeHurtBy
func (it *Item) hurt(damage float32, damageType registry.DamageType, events *[]ItemEvent) bool {
	return it.hurtBy(damage, damageType, false, events)
}

// Hurt applies a typed hurt and reports whether it landed.
// vanilla: ItemEntity.hurtServer
func (it *Item) Hurt(damage float32, damageType registry.DamageType, creative bool) bool {
	return it.hurtBy(damage, damageType, creative, nil)
}

func (it *Item) hurtBy(damage float32, damageType registry.DamageType, creative bool, events *[]ItemEvent) bool {
	if it.Dead || ((it.Invulnerable || it.InvulnerableTime > 0) && !creative && !rules.InvulnerabilityBypassed(damageType)) {
		return false
	}
	if (damageType.InTag(registry.DamageTypeTagIDIsFire) && it.fireImmune()) ||
		registry.StackResistsDamage(it.Stack, damageType) {
		return false
	}
	if it.HurtAsk != nil {
		it.HurtAsk(damage, damageType, it.Health)
	}
	return it.lose(damage, events)
}

// lose spends health and reports a destroyed stack.
// vanilla: ItemEntity.hurtServer
func (it *Item) lose(damage float32, events *[]ItemEvent) bool {
	it.Health = int(float32(it.Health) - damage)
	if events != nil {
		*events = append(*events, ItemEvent{Kind: ItemHurt})
	}
	if it.Health <= 0 {
		it.Dead = true
		if events != nil {
			*events = append(*events, ItemEvent{Kind: ItemDestroyed})
		}
	}
	return true
}

// BlastBox is the item's quarter-block box about its centre, for
// ServerExplosion.getSeenPercent.
func (it *Item) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return it.X - halfSize, it.Y - halfSize, it.Z - halfSize,
		it.X + halfSize, it.Y + halfSize, it.Z + halfSize
}

// FeetPosition is the entity position, the box's bottom centre, which blast
// distance is measured from.
func (it *Item) FeetPosition() (x, y, z float64) {
	return it.X, it.Y - halfSize, it.Z
}

// EyePosition is getEyePosition at the type's eye height of 0.2125, where an
// explosion's knockback is directed from.
func (it *Item) EyePosition() (x, y, z float64) {
	return it.X, it.Y - halfSize + itemEyeHeight, it.Z
}

// HurtByBlast is ItemEntity.hurtServer for a blast of damageType: gated by
// the stack's damage_resistant (the nether star's is_explosion), health
// spent in vanilla's float arithmetic and floored back to the integer. It
// reports whether the item died.
func (it *Item) HurtByBlast(damage float64, damageType registry.DamageType) bool {
	return it.HurtByCausedBlast(damage, damageType, false)
}

// HurtByCausedBlast reports whether a blast destroyed the item.
func (it *Item) HurtByCausedBlast(damage float64, damageType registry.DamageType, creative bool) bool {
	return it.Hurt(float32(damage), damageType, creative) && it.Dead
}

// Push is Entity.push: adds the vector and marks the item for a packet, so
// an explosion's push goes out as Set Entity Motion the next tick.
func (it *Item) Push(vx, vy, vz float64) {
	it.VX += vx
	it.VY += vy
	it.VZ += vz
	it.syncNeeded = true
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (it *Item) CapFall() { CapFallAccumulation(it.VY, &it.fallDistance) }

// SetFallDistance and FallDistance give tests the banked fall.
func (it *Item) SetFallDistance(d float64) { it.fallDistance = d }

// FallDistance is the fall banked since the item last stood on the ground.
func (it *Item) FallDistance() float64 { return it.fallDistance }

// Step advances the item one tick (ItemEntity.tick); near stands in for
// vanilla's box query, returning the world's items that may meet the item's
// merge box, in tick order (nil: none). In water it floats, in lava it
// burns, netherite does neither. Order: fluid interaction, fire ticks,
// pickup delay, movement, move, block effects, drag, the merge scan (every
// 2nd tick after crossing into another block, every 40th otherwise), and a
// second fluid read that applies a current again.
func (it *Item) Step(w World, near func(*Item) []*Item) []ItemEvent {
	if it.Dead {
		return nil
	}
	// vanilla: Entity.checkBelowWorld
	if it.Y < float64(w.MinY())-64 {
		it.Dead = true
		return nil
	}
	var events []ItemEvent
	// vanilla: Entity.commonTick
	if it.InvulnerableTime > 0 {
		it.InvulnerableTime--
	}
	it.tickCount++

	// The fluid pass vanilla's base tick opens with.
	inWater, inLava, waterHeight, lavaHeight := it.updateFluids(w, true)
	if inWater && !it.wasInWater && it.tickCount > 1 {
		events = append(events, it.splash())
	}
	it.wasInWater = inWater
	oldVX, oldVY, oldVZ := it.VX, it.VY, it.VZ

	// xo/yo/zo, the feet position before the move, for the merge schedule's
	// crossed-a-block check.
	oldX, oldY, oldZ := it.X, it.Y-halfSize, it.Z

	// Fire ticks run down by one, biting one damage every 20th tick unless
	// still in the lava that lit it; fireproof stacks never burn.
	if it.fireTicks > 0 {
		if it.fireImmune() {
			it.fireTicks = 0
		} else {
			if it.fireTicks%20 == 0 && !inLava {
				it.hurt(onFireDamage, rules.DamageTypeOnFire, &events)
			}
			it.fireTicks--
		}
	}
	it.fireFlag = it.fireTicks > 0

	if it.PickupDelay > 0 && it.PickupDelay != pickupDelayInfinite {
		it.PickupDelay--
	}

	// Water drift, lava drift, or gravity: fluid heights are compared at 0.1
	// above the feet (vanilla's threshold), so a barely dipped item falls rather
	// than floats.
	switch {
	case inWater && waterHeight > floatHeight:
		it.setFluidMovement(fluidDrag)
	case inLava && lavaHeight > floatHeight:
		it.setFluidMovement(lavaDrag)
	default:
		if !it.NoGravity {
			it.VY -= gravity
		}
	}

	// A box inside a block's collision gets vanilla's noPhysics for the
	// tick: the push toward the nearest free side replaces the velocity and
	// the move is raw and no block effect, rain or fire reset applies. The
	// flag is recomputed each tick, so it lapses as soon as the box is out.
	// vanilla: Entity.isAffectedByBlocks
	stuck := it.insideSolid(w)
	if stuck {
		it.moveTowardsClosestSpace(w)
	}

	// Move and drag: a grounded item with its drift spent moves only every
	// fourth tick (by entity id), the rest costing only age. Air drag takes
	// its share of every axis when it moves, ground friction of the
	// horizontals.
	if !it.OnGround || it.VX*it.VX+it.VZ*it.VZ > restThreshold || (it.tickCount+int(it.ID))%4 == 0 {
		fx, fy, fz, ax, az := it.X, it.Y, it.Z, it.VX, it.VZ
		if stuck {
			it.SetPos(it.X+it.VX, it.Y+it.VY, it.Z+it.VZ)
		} else {
			it.move(w)
			if it.bouncedOff {
				events = append(events, ItemEvent{Kind: ItemBounced})
			}
		}
		// A raw move records nothing, so the walk is the straight line from
		// the tick's start.
		it.lastMove = bodyMove{fx, fy, fz, it.X, it.Y, it.Z, ax, az, !stuck}
		it.hasLastMove = true
		if !stuck {
			it.insideEffects(w, &events)
		}
		g := airDragFloat
		if it.OnGround {
			g *= groundFrictionBelow(w, it.X, it.Y-halfSize, it.Z, halfSize)
		}
		it.VX *= g
		it.VY *= airDragFloat
		it.VZ *= g
		if it.OnGround && it.VY < 0 {
			it.VY = -it.VY * 0.5
		}
	} else if !stuck {
		it.insideEffects(w, &events)
	}

	// The merge scan on its schedule (every 2nd tick moving, 40th settled),
	// only for an item that can take more; from there it is
	// mergeWithNeighbours.
	if it.tickCount%mergeScanPeriod(it.crossedABlockThisTick(oldX, oldY, oldZ)) == 0 {
		it.mergeWithNeighbours(near)
	}

	// An item at the unlimited-lifetime marker never ages.
	// vanilla: ItemEntity.tick
	if it.Age != ItemUnlimitedAge {
		it.Age++
	}

	// The closing fluid pass: the same read at the new position, applying
	// the current again, and setting the sync flag.
	it.updateFluids(w, true)
	if dx, dy, dz := it.VX-oldVX, it.VY-oldVY, it.VZ-oldVZ; dx*dx+dy*dy+dz*dz > 0.01 {
		it.syncNeeded = true
	}
	if it.water.height > 0 || it.lava.height > 0 {
		it.syncNeeded = true
	}

	return events
}

// groundFrictionBelow is Block.getFriction for the block under feet at
// feetY. The column is the supporting block's (StandingBlock), so a body on
// an edge reads what it rests on; the row is just under half a block below
// the feet, so a body on a cell's floor reads that cell. The Java float is
// widened as elsewhere here.
// vanilla: Entity.getBlockPosBelowThatAffectsMyMovement, Block.getFriction
func groundFrictionBelow(w World, x, feetY, z, halfWidth float64) float64 {
	bx, _, bz := rules.StandingBlock(w, x, feetY, z, halfWidth)
	state := w.BlockAt(bx, int32(math.Floor(feetY-0.500001)), bz)
	return float64(float32(rules.BlockFriction(state)))
}

// slimeStep scales a grounded body's horizontal velocity by SlimeStepScale
// when it stands on slime. A body with no sideways drift skips the on-pos
// read, which the scale could not change.
// vanilla: SlimeBlock.stepOn, Entity.getOnPosLegacy
func slimeStep(w World, x, feetY, z, halfWidth, vy float64, vx, vz *float64) {
	if *vx == 0 && *vz == 0 {
		return
	}
	slimeStepOn(rules.OnPos(w, x, feetY, z, halfWidth), vy, vx, vz)
}

// unlitOreUnder is the stepOn of RedStoneOreBlock for a body resting on the
// ground: the cell under the feet at y, when it is unlit redstone ore.
// vanilla: Entity.applyEffectsFromBlocks, RedStoneOreBlock.stepOn
func unlitOreUnder(w World, x, y, z, halfWidth float64) (cx, cy, cz int32, ok bool) {
	cx, cy, cz, state := rules.OnPosCell(w, x, y, z, halfWidth)
	return cx, cy, cz, rules.IsRedstoneOre(state) && !registry.StateLit(state)
}

// slimeStepOn is slimeStep for a body whose on-pos state is already read.
func slimeStepOn(state uint16, vy float64, vx, vz *float64) {
	if s, ok := rules.SlimeStepScale(vy); ok && registry.BlockOf(state) == registry.BlockSlimeBlock {
		*vx *= s
		*vz *= s
	}
}

// gravityOf is the item's per-tick gravity, nothing for a body without it.
func gravityOf(noGravity bool) float64 {
	if noGravity {
		return 0
	}
	return gravity
}

// bounceOffGround is the vertical velocity after landing: the supporting
// block's restitution of the landing speed, else zero. A positive result is
// a bounce, which vanilla sends at once.
// vanilla: Entity.restituteMovementAfterCollisions, Entity.getOnPos
func bounceOffGround(w World, x, feetY, z, halfWidth, vy, moved, g, airDrag float64, living bool) float64 {
	if -vy <= g {
		return 0 // BouncedFall's cut-off, ahead of the support lookup
	}
	state := rules.OnPos(w, x, feetY, z, halfWidth)
	return rules.BouncedFall(vy, moved, g, airDrag, rules.BlockBounceRestitution(state, living))
}

// setFluidMovement is ItemEntity.setFluidMovement: sideways drift shrunk by
// the fluid's drag, the vertical lifted until it rises as fast as allowed.
func (it *Item) setFluidMovement(multiplier float64) {
	it.VX *= multiplier
	if it.VY < fluidRiseCap {
		it.VY += fluidRise
	}
	it.VZ *= multiplier
}

// move moves the item along its velocity as far as blocks allow, vertical
// first then horizontal larger first, as vanilla's move and collide do,
// setting OnGround.
func (it *Item) move(w World) {
	it.OnGround, it.bouncedOff = false, false
	ctx, startY := fallCtx(it.fallDistance), it.Y
	// vanilla: Entity.move spends the stuck multiplier and the velocity.
	dx, dy, dz, webbed := it.webbed.scale(it.VX, it.VY, it.VZ)
	if webbed {
		it.VX, it.VY, it.VZ = 0, 0, 0
	}
	cells := gatherMoveCells(w, it.itemBox(), dx, dy, dz, 0)
	defer cells.release()
	ctx.moveCells = &cells
	if dy < 0 {
		oldY := it.Y
		landY, blocked := clipFall(w, ctx, it.X, it.Z, halfSize, it.Y-halfSize, dy)
		it.Y = landY + halfSize
		if blocked {
			it.VY = bounceOffGround(w, it.X, it.Y-halfSize, it.Z, halfSize, it.VY, it.Y-oldY, gravityOf(it.NoGravity), airDragFloat, false)
			if it.VY > 0 {
				it.syncNeeded = true
				it.bouncedOff = true
			}
			it.OnGround = true
		}
	} else if dy > 0 {
		topY, blocked := clipRise(w, ctx, it.X, it.Z, halfSize, it.Y+halfSize, dy)
		it.Y = topY - halfSize
		if blocked {
			it.VY = 0
		}
	}
	if dx != 0 || dz != 0 {
		box, _, blockedX, blockedZ := collideHorizontal(w, ctx, it.itemBox(), dx, dz)
		cx, cz := box.centerXZ()
		it.SetPos(cx, it.Y, cz)
		if blockedX {
			it.VX = 0
		}
		if blockedZ {
			it.VZ = 0
		}
	}
	// vanilla: Entity.move ends with the block's speed factor and the stuck
	// multiplier the cells the box ended in leave for the next move.
	f := blockSpeedFactor(w, it.X, it.Y-halfSize, it.Z)
	it.VX *= f
	it.VZ *= f
	it.LandedFall = bankFall(&it.fallDistance, startY, it.Y, it.OnGround)
	it.webbed = stuckIn(w, it.itemBox(), it.X, it.Y-halfSize, it.Z, false, false, false, false)
}

// insideSolid is the vanilla tick's level.noCollision on the deflated box
// (negated, it is the noPhysics flag): the box shaved by the physics
// epsilon meets a block shape or collidable body. A face exactly touching
// shaves clear. Cells one below are read too, since a shape can rise past
// its cell (a fence's 24/16).
func (it *Item) insideSolid(w World) bool {
	box := it.itemBox()
	box.deflate(physicsEpsilon)
	return overlapsSolid(w, noCtx, box)
}

// moveTowardsClosestSpace is Entity.moveTowardsClosestSpace for the item:
// the walk is measured from the box's centre.
func (it *Item) moveTowardsClosestSpace(w World) {
	it.VX, it.VY, it.VZ = moveTowardsClosestSpace(w, it.X, it.Y, it.Z, it.VX, it.VY, it.VZ, it.dice())
}

// insideEffects is the item's applyEffectsFromBlocks: water and rain
// extinguish, lava and fire ignite and hurt (fire more often), cactus
// hurts. Each cell the box swept on its last move is asked once; sounds
// come back as events.
func (it *Item) insideEffects(w World, events *[]ItemEvent) {
	// stepOn comes first, between the move and the ground friction.
	// vanilla: Entity.applyEffectsFromBlocks
	if it.OnGround {
		x, y, z, state := rules.OnPosCell(w, it.X, it.Y-halfSize, it.Z, halfSize)
		slimeStepOn(state, it.VY, &it.VX, &it.VZ)
		if rules.IsRedstoneOre(state) && !registry.StateLit(state) {
			*events = append(*events, ItemEvent{Kind: ItemSteppedOnRedstoneOre, X: x, Y: y, Z: z})
		}
	}
	wasOnFire := it.isOnFire()
	previousFireTicks := it.fireTicks
	var spots []bubbleSpot
	var cauldrons [][3]int32
	var moves []bodyMove
	if it.hasLastMove {
		moves = []bodyMove{it.lastMove}
	}
	rel := AABB{-halfSize, -halfSize, -halfSize, halfSize, halfSize, halfSize}
	walkInside(w, moves, rel, func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
		if vy, ok := projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, it.VY, true, &spots); ok {
			it.VY = vy
			for _, p := range spots {
				*events = append(*events, ItemEvent{Kind: ItemBubbleSurface, X: p.x, Y: p.y, Z: p.z})
			}
			spots = spots[:0]
		}
		fluid := sweptFluid(w, x, y, z, state, from, dx, dy, dz)
		switch {
		case rules.IsLayeredCauldron(state):
			cauldrons = append(cauldrons, [3]int32{x, y, z})
		case fluid == fluidWater:
			it.fireTicks = 0
		case fluid == fluidLava:
			// The lava's two effects in vanilla's order: the burn lit
			// for fifteen seconds, then the hurt and its sound.
			if !it.fireImmune() && it.fireTicks < fireTicksLava {
				it.fireTicks = fireTicksLava
			}
			if it.hurt(lavaDamage, rules.DamageTypeLava, events) && (it.Health <= 0 || it.tickCount%10 == 0) {
				*events = append(*events, ItemEvent{Kind: ItemBurned})
			}
		case rules.IsFire(state):
			// A fire block lights an eight-second burn and bites once a
			// tick for its own damage — the soul fire's double.
			damage := fireBlockDamage
			if rules.IsSoulFire(state) {
				damage = soulFireDamage
			}
			if !it.fireImmune() && it.fireTicks < fireTicksFire {
				it.fireTicks = fireTicksFire
			}
			it.hurt(damage, rules.DamageTypeInFire, events)
		case rules.IsHoneyBlock(state):
			// An item slides but plays no sound and sends no event.
			// vanilla: HoneyBlock.entityInside, doesEntityDoHoneyBlockSlideEffects
			if rules.HoneySliding(x, y, z, it.X, it.Y-halfSize, it.Z, it.VY, size, it.OnGround) {
				it.VX, it.VY, it.VZ = rules.HoneySlide(it.VX, it.VY, it.VZ)
				it.fallDistance = 0
			}
		case rules.IsCactus(state):
			// The cactus is no fire damage, so a fireproof stack takes it.
			// vanilla: CactusBlock.entityInside
			it.hurt(cactusDamage, rules.DamageTypeCactus, events)
		case rules.IsPowderSnow(state):
			// The movement read is after the move, which spends it when
			// the snow held the item from the tick before: only the tick
			// the item comes in kicks up flakes.
			if it.VX != 0 || it.VZ != 0 {
				*events = append(*events, ItemEvent{Kind: ItemSnowflake, X: x, Y: y, Z: z})
			}
		}
		return !it.Dead // vanilla: the walk stops for a body no longer alive
	})
	// Every cauldron in the box drops a level while the item is still on
	// fire as the pass ends, before the rain.
	// vanilla: LayeredCauldronBlock.entityInside
	if it.isOnFire() {
		for _, c := range cauldrons {
			it.fireTicks = 0
			*events = append(*events, ItemEvent{Kind: ItemDousedCauldron, X: c[0], Y: c[1], Z: c[2]})
		}
	}
	if b := it.itemBox(); rainLandsOn(w, it.X, b.minY, it.Z, b.maxY-b.minY) {
		it.fireTicks = 0
	}
	if wasOnFire && !it.isOnFire() {
		*events = append(*events, ItemEvent{Kind: ItemExtinguished})
	}
	// An item neither burning nor lit this pass rests at -1 (minus
	// getFireImmuneTicks), so a bolt's +1 brings it to 0 and ignites it.
	// vanilla: Entity.applyEffectsFromBlocks
	if !it.isOnFire() && it.fireTicks <= previousFireTicks {
		it.fireTicks = -1
	}
}

// mergeScanPeriod is the rhythm the merge scan keeps — the period the
// vanilla tick picks between its 2 and its 40 by the moved flag.
func mergeScanPeriod(moved bool) int {
	if moved {
		return 2
	}
	return 40
}

// crossedABlockThisTick compares the floored positions before and after the
// move on all three axes, as the vanilla tick does; Y uses the feet.
func (it *Item) crossedABlockThisTick(oldX, oldY, oldZ float64) bool {
	return math.Floor(oldX) != math.Floor(it.X) ||
		math.Floor(oldY) != math.Floor(it.Y-halfSize) ||
		math.Floor(oldZ) != math.Floor(it.Z)
}

// isMergable is ItemEntity.isMergable, asked of both parties: alive, not
// never-pickable (pickup delay 32767), younger than the despawn and not the
// unlimited marker, with room for one more.
func (it *Item) isMergable() bool {
	return !it.Dead && it.PickupDelay != pickupDelayInfinite &&
		it.Age != ItemUnlimitedAge && it.Age < despawnTicks &&
		it.Stack.Count < registry.StackMaxStackSize(it.Stack)
}

// areMergable is ItemEntity.areMergable: the combined count fits the first's
// maximum and isSameItemSameComponents holds (an enchanted sword never joins
// a plain one).
func areMergable(a, b protocol.ItemStack) bool {
	if b.Count+a.Count > registry.StackMaxStackSize(a) {
		return false
	}
	return a.Same(b)
}

// mergeWithNeighbours is ItemEntity.mergeWithNeighbours: the items near
// returns are tried in turn until this item is merged into one.
func (it *Item) mergeWithNeighbours(near func(*Item) []*Item) {
	if near == nil || !it.isMergable() {
		return
	}
	for _, other := range near(it) {
		if other == nil || other == it || other.Dead {
			continue
		}
		if !other.isMergable() || !mergeBoxesMeet(it, other) {
			continue
		}
		it.tryToMerge(other)
		if it.Dead {
			return
		}
	}
}

// sameTarget reports whether two items agree on who may pick them up.
// vanilla: ItemEntity.tryToMerge
func sameTarget(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// tryToMerge is ItemEntity.tryToMerge: the larger stack survives (the
// neighbour on a tie), keeping its position, taking the longer pickup delay
// and younger age, and marking itself; the other shrinks by what was taken
// and is discarded when empty.
func (it *Item) tryToMerge(other *Item) {
	if !sameTarget(it.Target, other.Target) || !areMergable(it.Stack, other.Stack) {
		return
	}
	survivor, victim := it, other
	if other.Stack.Count >= it.Stack.Count {
		survivor, victim = other, it
	}
	if it.MergeAsk != nil && it.MergeAsk(survivor, victim) {
		return
	}
	MergeStacks(survivor, victim)
}

// MergeStacks moves what fits of the victim's stack into the survivor's.
func MergeStacks(survivor, victim *Item) {
	// The three-argument merge: the survivor takes what fits under its
	// maximum, capped at 64 as vanilla's call passes.
	room := min(registry.StackMaxStackSize(survivor.Stack), 64) - survivor.Stack.Count
	take := min(room, victim.Stack.Count)
	survivor.Stack.Count += take
	victim.Stack.Count -= take
	survivor.PickupDelay = max(survivor.PickupDelay, victim.PickupDelay)
	survivor.Age = min(survivor.Age, victim.Age)
	survivor.mergeTaken = true
	if victim.Stack.Empty() {
		victim.Dead = true
	}
}

// mergeBoxesMeet reports whether b's box meets a's grown by the scan's
// AABB.inflate(0.5, 0.0, 0.5), with AABB.intersects' strict test (touching
// does not count).
func mergeBoxesMeet(a, b *Item) bool {
	h, g := halfSize, mergeInflate
	return a.X+h+g > b.X-h && a.X-h-g < b.X+h &&
		a.Y+h > b.Y-h && a.Y-h < b.Y+h &&
		a.Z+h+g > b.Z-h && a.Z-h-g < b.Z+h
}

// ShouldDespawn reports whether the item's five minutes are up.
func (it *Item) ShouldDespawn() bool {
	return it.Age >= despawnTicks
}

// PickupDue reports whether the pickup delay has run out.
func (it *Item) PickupDue() bool {
	return it.PickupDelay == 0
}

// splash is the ItemSplashed of an item entering water.
// vanilla: Entity.doWaterSplashEffect
func (it *Item) splash() ItemEvent {
	volume, pitch := splashSound(it.VX, it.VY, it.VZ, it.dice())
	return ItemEvent{Kind: ItemSplashed, Volume: volume, Pitch: pitch}
}

// splashSound is the volume and pitch of an unridden body's water-entry
// splash at velocity vx, vy, vz; a nil r plays pitch 1.
// vanilla: Entity.doWaterSplashEffect
func splashSound(vx, vy, vz float64, r *rand.Rand) (volume, pitch float32) {
	volume = min(1, float32(math.Sqrt(vx*vx*0.2+vy*vy+vz*vz*0.2))*0.2)
	if r == nil {
		return volume, 1
	}
	return volume, 1 + (r.Float32()-r.Float32())*0.4
}
