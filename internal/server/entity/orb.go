package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The experience orb, from the ExperienceOrb bytecode. A plain value
// like items and arrows: players are passed in and events returned.
//
// X, Y, Z is the box's bottom centre (the feet); the box is
// EXPERIENCE_ORB's sized(0.5, 0.5).

// ExperienceOrb's numbers: gravity 0.03 (getDefaultGravity), air drag 0.98
// (getAirDrag), times the block's friction when resting (getFriction's 0.6
// for ordinary ground); a landing still falling bounces with 0.4 of its
// speed (verticalCollisionBelow); it follows the nearest player within
// MAX_FOLLOW_DIST's 8 blocks with pull (share of the way)² ×
// followNearbyPlayer's 0.1; the merge scan runs every ENTITY_SCAN_PERIOD
// (20) ticks, merging neighbours whose boxes grown by ORB_MERGE_DISTANCE's
// 0.5 touch with matching value and ids; LIFETIME is 6000. Fluid numbers are
// in fluids.go.
const (
	orbGravity        = 0.03
	orbAirDrag        = airDragFloat
	orbBounceFactor   = 0.4
	orbFollowRange    = 8.0
	orbFollowStrength = 0.1
	orbScanPeriod     = 20
	orbMergeDistance  = 0.5
	orbLifeLimit      = 6000
	orbHalfSize       = 0.25
	// OrbHalfSize is half the orb's box width.
	OrbHalfSize = orbHalfSize
	orbHeight   = 0.5
	// OrbMergeReach is the largest X or Z distance between two orbs' centres
	// at which orbBoxesTouch can hold.
	OrbMergeReach = 2*orbHalfSize + orbMergeDistance

	// OrbTracking and OrbInterval are the builder's own clientTrackingRange
	// and updateInterval for the type, six chunks and twenty ticks.
	OrbTracking = 6
	OrbInterval = 20
)

// standingEyeHeight is a standing player's unscaled eye height, used when a
// view carries none.
const standingEyeHeight = 1.62

// OrbPlayer is a player as an orb sees them: feet position by entity ID,
// spectator (never followed), dead (chosen as nearest, then refused), and
// whether they may take an orb this tick (takeXpDelay done).
type OrbPlayer struct {
	X, Y, Z   float64
	EntityID  int32
	Spectator bool
	Dead      bool
	TakeReady bool
	// Reach is the box the player touches orbs within.
	Reach AABB
	// EyeHeight is the eye height by pose and scale; zero means standing.
	EyeHeight float64
}

// OrbEventKind is one thing an orb's tick produced that the server may want
// to tell the world about or act on.
type OrbEventKind uint8

const (
	// OrbTaken: a player touched the orb (playerTouch). Collector is the taker;
	// the value is one pickup's whole worth, before mending takes its share.
	OrbTaken OrbEventKind = iota
	// OrbExpired: the orb's six thousand ticks ran out or a burn spent the
	// last of its health — vanilla's own discard.
	OrbExpired
	// OrbBurned: a lava-or-fire hurt played the burn sound the orb's
	// shouldPlayLavaHurtSound asks for of every one.
	OrbBurned
	// OrbExtinguished: a burn put out by water or rain, and vanilla plays
	// the extinguish sound for it.
	OrbExtinguished
	// OrbSnowflake: the orb moved sideways in the powder snow cell at Y,
	// and may kick up a snowflake above it.
	OrbSnowflake
	// OrbSteppedOnRedstoneOre: the orb rests on the unlit redstone ore at
	// X, Y, Z.
	OrbSteppedOnRedstoneOre
	// OrbDousedCauldron: a burning orb was put out in the layered cauldron
	// at X, Y, Z, which drops a level.
	OrbDousedCauldron
	// OrbMeltedSnow: a burning orb was put out in the powder snow at X, Y, Z,
	// which breaks.
	OrbMeltedSnow
)

// OrbEvent is one event out of an orb's tick. The Collector rides only an
// OrbTaken, Y only an OrbSnowflake, X, Y and Z an OrbSteppedOnRedstoneOre.
type OrbEvent struct {
	Kind      OrbEventKind
	Collector int32
	X, Y, Z   int32
}

// Orb is one orb in a world. Value is one pickup's worth (metadata), Count
// the pickups it holds (merging adds, touching spends one). Age counts to
// 6000. OnGround is the last step's result, read by the next step's drag
// and saved. Dead marks one spent, merged, expired or burned. Health is the
// five points burns spend; Rand rolls the lava bounce.
type Orb struct {
	waterEntry

	// Splash is the water entry of the last Step, nil for none.
	Splash *WaterSplash

	ID int32
	Place
	VX, VY, VZ float64
	Value      int
	Count      int
	Age        int
	OnGround   bool
	// fallDistance is the fall banked since the orb last stood on the ground.
	fallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none.
	LandedFall float64
	// stuck is the multiplier the last block effects left for the next
	// move.
	stuck  stuckFactor
	Health int
	Dead   bool
	// Invulnerable is Entity.invulnerable: ExperienceOrb.hurtServer returns
	// early on isInvulnerableToBase.
	Invulnerable bool
	Rand         *rand.Rand
	// vanilla: Entity.isNoGravity
	NoGravity bool

	// water and lava are the orb's EntityFluidInteraction, refilled once a
	// tick before it moves.
	water, lava fluidTracker

	// fireTicks is the burn still to run, vanilla's remainingFireTicks.
	fireTicks int

	// syncNeeded is needsSync, set by an explosion's push so the next pass sends
	// the velocity whatever the movement interval would do.
	syncNeeded bool

	tickCount      int
	followingID    int32
	followingKnown bool
}

// NewOrb places an orb worth value with one pickup, five health, and its own
// random source from seed.
func NewOrb(id int32, x, y, z, vx, vy, vz float64, value int, seed int64) *Orb {
	return &Orb{
		ID: id,
		X:  x, Y: y, Z: z,
		VX: vx, VY: vy, VZ: vz,
		Value:  value,
		Count:  1,
		Health: 5,
		Rand:   NewDice(seed),
	}
}

// dice is the orb's own random source, made from its ID when the one the
// constructor gave has been left unset.
func (o *Orb) dice() *rand.Rand {
	if o.Rand == nil {
		o.Rand = NewDice(int64(o.ID))
	}
	return o.Rand
}

// gravity is Entity.getGravity: the 0.03 a tick an orb falls at, and nought
// for an orb Entity.isNoGravity holds up.
func (o *Orb) gravity() float64 {
	if o.NoGravity {
		return 0
	}
	return orbGravity
}

// hurt is ExperienceOrb.hurtServer for fire and lava: health takes the
// damage and an emptied orb is discarded (reported as an event).
func (o *Orb) hurt(damage float32, events *[]OrbEvent) {
	if o.Invulnerable {
		return
	}
	o.Health = int(float32(o.Health) - damage)
	if o.Health <= 0 {
		o.Dead = true
		*events = append(*events, OrbEvent{Kind: OrbExpired})
	}
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (o *Orb) CapFall() { CapFallAccumulation(o.VY, &o.fallDistance) }

// BlastBox is the bounding box the exposure of a blast is measured over —
// the orb's own half-block box, ServerExplosion.getSeenPercent's shape.
func (o *Orb) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return o.X - orbHalfSize, o.Y, o.Z - orbHalfSize,
		o.X + orbHalfSize, o.Y + orbHeight, o.Z + orbHalfSize
}

// EyePosition is getEyePosition, where an explosion's knockback is directed
// from: EntityDimensions' default eye height, 0.85 of the box's, 0.425 for
// the half-block orb.
func (o *Orb) EyePosition() (x, y, z float64) {
	return o.X, o.Y + orbEyeHeight, o.Z
}

// Hurt is ExperienceOrb.hurtServer: health spent in vanilla's float
// arithmetic and floored to the integer, destroyed at zero or less. An
// invulnerable orb refuses the hit unless bypass (Entity.isInvulnerableTo).
// It reports whether the hit landed and whether the orb died.
func (o *Orb) Hurt(damage float64, bypass bool) (landed, dead bool) {
	if o.Invulnerable && !bypass {
		return false, false
	}
	o.Health = int(float32(o.Health) - float32(damage))
	if o.Health <= 0 {
		o.Dead = true
	}
	return true, o.Dead
}

// Push is Entity.push: adds the vector and marks the orb for a packet, so an
// explosion's push goes out as Set Entity Motion the next tick.
func (o *Orb) Push(vx, vy, vz float64) {
	o.VX += vx
	o.VY += vy
	o.VZ += vz
	o.syncNeeded = true
}

// TakeSync reports and clears syncNeeded, as the vanilla server consumes
// Entity.push's mark.
func (o *Orb) TakeSync() bool {
	if o.syncNeeded {
		o.syncNeeded = false
		return true
	}
	return false
}

// Step advances the orb one tick (ExperienceOrb.tick) with its dimension's
// players; near returns the world's orbs that may touch its merge box, in
// tick order (nil: none). Vanilla's order:
// fluids and current, fire countdown, drift or gravity, lava bounce, merge
// scan on its period, follow, move, block effects, drag, bounce, age and
// despawn, then pickup.
func (o *Orb) Step(w World, players []OrbPlayer, near func(*Orb) []*Orb) []OrbEvent {
	o.Splash = nil
	if o.Dead {
		return nil
	}
	var events []OrbEvent
	// The base tick's below-world check: 64 below the floor it is discarded
	// (onBelowWorld).
	if o.Y < float64(w.MinY())-64 {
		o.Dead = true
		return []OrbEvent{{Kind: OrbExpired}}
	}
	o.tickCount++

	// The fluid pass vanilla's base tick runs.
	o.Splash = o.step(w, o.orbBox(), o.VX, o.VY, o.VZ)
	_, inLava := o.updateFluids(w)

	// The fire ticks, the same twentieth-tick bite the items' carry.
	if o.fireTicks > 0 {
		if o.fireTicks%20 == 0 && !inLava {
			o.hurt(onFireDamage, &events)
		}
		o.fireTicks--
	}

	// Water drift (by the eye-in-water test) or gravity; lava is looked for
	// at the feet cell, where vanilla looks. A body wedged in blocks gets no
	// gravity, so a stuck orb stops sinking.
	colliding := o.colliding(w)
	switch {
	case o.water.eyesIn:
		o.setUnderwaterMovement()
	case !colliding:
		o.VY -= o.gravity()
	}

	// The lava's bounce: thrown up out of the pool on a random sideways
	// wander, every tick it stands in one.
	if o.inLavaCell(w) {
		dice := o.dice()
		o.VX = float64((dice.Float32() - dice.Float32()) * float32(0.2))
		o.VY = float64(float32(0.2))
		o.VZ = float64((dice.Float32() - dice.Float32()) * float32(0.2))
	}

	// The merge scan on its period: each neighbour of the same value, whose id
	// differs by a multiple of 40 and whose box grown by 0.5 touches, is
	// absorbed: its pickups added, the younger age kept, the neighbour dead.
	if o.tickCount%orbScanPeriod == 1 && near != nil {
		for _, other := range near(o) {
			if other == nil || other == o || other.Dead {
				continue
			}
			if other.Value != o.Value || (other.ID-o.ID)%40 != 0 {
				continue
			}
			if !orbBoxesTouch(o, other) {
				continue
			}
			o.Count += other.Count
			if other.Age < o.Age {
				o.Age = other.Age
			}
			other.Dead = true
		}
	}

	// Follow: keep the current player while within eight blocks and
	// followable, else find the nearest.
	o.followNearbyPlayer(players)

	// A trapped orb with no follower whose next move would still cross a
	// block is pushed toward the nearest free side and marked for a packet.
	if !o.followingKnown && colliding && overlapsSolid(w, noCtx, o.orbBox().moved(o.VX, o.VY, o.VZ)) {
		o.VX, o.VY, o.VZ = moveTowardsClosestSpace(w, o.X, o.Y+orbHeight/2, o.Z, o.VX, o.VY, o.VZ, o.dice())
		o.syncNeeded = true
	}

	// The move records OnGround, which the drag below and the save read.
	beforeY := o.VY
	fx, fy, fz, ax, az := o.X, o.Y, o.Z, o.VX, o.VZ
	o.OnGround = o.move(w)

	// The effects of the blocks the move carried the orb through.
	o.insideEffects(w, bodyMove{fx, fy, fz, o.X, o.Y, o.Z, ax, az, true}, &events)

	// The drag: the air's, times the friction of the ground the orb rests
	// on.
	drag := orbAirDrag
	if o.OnGround {
		drag *= groundFrictionBelow(w, o.X, o.Y, o.Z, orbHalfSize)
	}
	o.VX *= drag
	o.VY *= drag
	o.VZ *= drag

	// Bounce: a landing still falling returns with 0.4 of its speed. Under
	// noPhysics the fall is measured against zero, so any downward speed
	// counts.
	if o.OnGround && beforeY < -o.gravity() {
		o.VY = -beforeY * orbBounceFactor
	}

	o.Age++
	if o.Age >= orbLifeLimit {
		o.Dead = true
		events = append(events, OrbEvent{Kind: OrbExpired})
		return events
	}

	// The pickup, the player touch a frozen world still runs on its own.
	return append(events, o.PlayerTouch(players)...)
}

// PlayerTouch gives one pickup to the first player who may take one this
// tick and whose grown box meets the orb's; the last pickup spends the orb.
// It is Step's pickup half, also run alone while a frozen world skips the
// orb's tick.
// vanilla: ExperienceOrb.playerTouch
func (o *Orb) PlayerTouch(players []OrbPlayer) []OrbEvent {
	if o.Dead {
		return nil
	}
	for _, p := range players {
		if !p.TakeReady || !o.WithinPickupRangeOf(p) {
			continue
		}
		o.Count--
		if o.Count <= 0 {
			o.Dead = true
		}
		return []OrbEvent{{Kind: OrbTaken, Collector: p.EntityID}}
	}
	return nil
}

// setUnderwaterMovement is the orb's own water drift (not the item's):
// sideways drag, and the vertical lifted up to the cap.
func (o *Orb) setUnderwaterMovement() {
	o.VX *= fluidDrag
	o.VY = math.Min(o.VY+fluidRise, fluidRiseCap)
	o.VZ *= fluidDrag
}

// inLavaCell reports whether the fluid in the cell the orb's feet stand in
// is lava, the check the vanilla tick's bounce branch makes.
func (o *Orb) inLavaCell(w World) bool {
	return fluidCellOf(w.BlockAt(int32(math.Floor(o.X)), int32(math.Floor(o.Y)), int32(math.Floor(o.Z)))).kind == fluidLava
}

// colliding is the noCollision test (inverted) that the vanilla tick's
// gravity branch gates on: the box meets a block shape or collidable body.
func (o *Orb) colliding(w World) bool {
	return overlapsSolid(w, noCtx, o.orbBox())
}

// insideEffects is the orb's applyEffectsFromBlocks over the cells the move
// swept: water extinguishes, lava and fire ignite and hurt; each cell asked
// once; sounds as events.
func (o *Orb) insideEffects(w World, move bodyMove, events *[]OrbEvent) {
	// vanilla: Entity.applyEffectsFromBlocks's stepOn
	if o.OnGround {
		slimeStep(w, o.X, o.Y, o.Z, orbHalfSize, o.VY, &o.VX, &o.VZ)
		if x, y, z, ok := unlitOreUnder(w, o.X, o.Y, o.Z, orbHalfSize); ok {
			*events = append(*events, OrbEvent{Kind: OrbSteppedOnRedstoneOre, X: x, Y: y, Z: z})
		}
	}
	o.VY, _ = bubbleColumnDrift(w, o.orbBox(), o.VY)
	wasOnFire := o.fireTicks > 0
	// The movement read is after the move, which spends it when the snow
	// held the orb from the tick before.
	sideways := o.VX != 0 || o.VZ != 0
	rel := AABB{-orbHalfSize, 0, -orbHalfSize, orbHalfSize, orbHeight, orbHalfSize}
	var cauldrons, snows [][3]int32
	walkInside(w, []bodyMove{move}, rel, func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
		if insideNonLiving(state, sideways, &o.stuck) {
			*events = append(*events, OrbEvent{Kind: OrbSnowflake, Y: y})
		}
		fluid := sweptFluid(w, x, y, z, state, from, dx, dy, dz)
		switch {
		case rules.IsLayeredCauldron(state):
			cauldrons = append(cauldrons, [3]int32{x, y, z})
		case rules.IsPowderSnow(state):
			snows = append(snows, [3]int32{x, y, z})
		case fluid == fluidWater:
			o.fireTicks = 0
		case fluid == fluidLava || sweptLavaCauldron(x, y, z, state, from, dx, dy, dz):
			if o.fireTicks < fireTicksLava {
				o.fireTicks = fireTicksLava
			}
			o.hurt(lavaDamage, events)
			if !o.Invulnerable {
				*events = append(*events, OrbEvent{Kind: OrbBurned})
			}
		case rules.IsFire(state):
			damage := fireBlockDamage
			if rules.IsSoulFire(state) {
				damage = soulFireDamage
			}
			if o.fireTicks < fireTicksFire {
				o.fireTicks = fireTicksFire
			}
			o.hurt(damage, events)
		}
		return !o.Dead // vanilla: the walk stops for a body no longer alive
	})
	// Both run before the extinguish, so a burning orb in each lowers the
	// cauldron and breaks the snow.
	// vanilla: LayeredCauldronBlock.entityInside, PowderSnowBlock.entityInside
	if o.fireTicks > 0 {
		for _, c := range cauldrons {
			*events = append(*events, OrbEvent{Kind: OrbDousedCauldron, X: c[0], Y: c[1], Z: c[2]})
		}
		for _, c := range snows {
			*events = append(*events, OrbEvent{Kind: OrbMeltedSnow, X: c[0], Y: c[1], Z: c[2]})
		}
		if len(cauldrons)+len(snows) > 0 {
			o.fireTicks = 0
		}
	}
	if rainLandsOn(w, o.X, o.Y, o.Z, orbHeight) {
		o.fireTicks = 0
	}
	if wasOnFire && o.fireTicks <= 0 {
		*events = append(*events, OrbEvent{Kind: OrbExtinguished})
	}
}

// FireTicks is the burn the orb still has to run.
func (o *Orb) FireTicks() int { return o.fireTicks }

// SetFire sets the burn, vanilla's remainingFireTicks.
func (o *Orb) SetFire(ticks int) { o.fireTicks = ticks }

// followNearbyPlayer is vanilla's followNearbyPlayer. The current player is
// kept unless a spectator or more than 8 blocks feet to feet (64 squared; a
// dead one is kept). Otherwise the nearest non-spectator within 8, dead
// included, is chosen and refused if dead. The pull is toward their middle
// height, scaled by the square of the remaining share of eight blocks and by
// 0.1.
func (o *Orb) followNearbyPlayer(players []OrbPlayer) {
	var followed *OrbPlayer
	if o.followingKnown {
		for i := range players {
			if players[i].EntityID == o.followingID {
				followed = &players[i]
				break
			}
		}
		if followed != nil {
			dx, dy, dz := followed.X-o.X, followed.Y-o.Y, followed.Z-o.Z
			if followed.Spectator || dx*dx+dy*dy+dz*dz > orbFollowRange*orbFollowRange {
				followed = nil
			}
		}
	}
	if followed == nil {
		// vanilla: EntityGetter.getNearestPlayer, feet to feet, strictly
		// inside the range.
		best := orbFollowRange * orbFollowRange
		for i := range players {
			p := &players[i]
			if p.Spectator {
				continue
			}
			dx, dy, dz := p.X-o.X, p.Y-o.Y, p.Z-o.Z
			dist := dx*dx + dy*dy + dz*dz
			if dist < best {
				best = dist
				followed = p
			}
		}
		if followed != nil && followed.Dead {
			followed = nil
		}
	}
	if followed == nil {
		o.followingKnown = false
		return
	}
	o.followingID = followed.EntityID
	o.followingKnown = true
	// The aim is the way to the player's feet plus half the eye height up.
	// vanilla: ExperienceOrb.tick
	eye := followed.EyeHeight
	if eye == 0 {
		eye = standingEyeHeight
	}
	aimX, aimY, aimZ := followed.X-o.X, followed.Y+eye/2-o.Y, followed.Z-o.Z
	length := math.Sqrt(aimX*aimX + aimY*aimY + aimZ*aimZ)
	if length > 0 {
		aimX, aimY, aimZ = aimX/length, aimY/length, aimZ/length
	}
	amount := (1 - length/orbFollowRange) * (1 - length/orbFollowRange) * orbFollowStrength
	o.VX += aimX * amount
	o.VY += aimY * amount
	o.VZ += aimZ * amount
}

// updateFluids is one EntityFluidInteraction.update: water and lava around
// the orb and a flowing one's push (water's strength, lava's per dimension).
func (o *Orb) updateFluids(w World) (inWater, inLava bool) {
	updateFluid(w, o.orbBox(), o.X, o.Y+orbEyeHeight, o.Z, true, &o.water, &o.lava)
	inWater = o.water.height > 0
	inLava = o.lava.height > 0
	if inWater {
		if dx, dy, dz, any := o.water.currentImpulse(waterCurrent, o.VX, o.VZ); any {
			o.VX += dx
			o.VY += dy
			o.VZ += dz
		}
	}
	if inLava {
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, any := o.lava.currentImpulse(scale, o.VX, o.VZ); any {
			o.VX += dx
			o.VY += dy
			o.VZ += dz
		}
	}
	return inWater, inLava
}

// move moves the orb along its velocity as far as blocks allow, vertical
// first then horizontal larger first, as vanilla's move and collide do. It
// reports whether a stopped fall left it on the ground.
func (o *Orb) move(w World) bool {
	o.OnGround = false
	ctx, startY := fallCtx(o.fallDistance), o.Y
	// vanilla: Entity.move spends the stuck multiplier and the velocity.
	dx, dy, dz, held := o.stuck.scale(o.VX, o.VY, o.VZ)
	if held {
		o.VX, o.VY, o.VZ = 0, 0, 0
	}
	if dy < 0 {
		landY, blocked := clipFall(w, ctx, o.X, o.Z, orbHalfSize, o.Y, dy)
		o.Y = landY
		if blocked {
			o.VY = 0
			o.OnGround = true
		}
	} else if dy > 0 {
		topY, blocked := clipRise(w, ctx, o.X, o.Z, orbHalfSize, o.Y+orbHeight, dy)
		o.Y = topY - orbHeight
		if blocked {
			o.VY = 0
		}
	}
	if dx != 0 || dz != 0 {
		box, _, blockedX, blockedZ := collideHorizontal(w, ctx, o.orbBox(), dx, dz)
		cx, cz := box.centerXZ()
		o.SetPos(cx, o.Y, cz)
		if blockedX {
			o.VX = 0
		}
		if blockedZ {
			o.VZ = 0
		}
	}
	// vanilla: Entity.move ends with the block speed factor
	f := blockSpeedFactor(w, o.X, o.Y, o.Z)
	o.VX *= f
	o.VZ *= f
	o.LandedFall = bankFall(&o.fallDistance, startY, o.Y, o.OnGround)
	return o.OnGround
}

// WithinPickupRangeOf is the per-tick pickup check: the player's reach box
// meets the orb's. vanilla: Player.aiStep
func (o *Orb) WithinPickupRangeOf(p OrbPlayer) bool {
	return p.Reach.Crosses(NewAABB(o.BlastBox()))
}

// WithinAwardMergeOf reports whether an award at the point finds the orb in
// tryMergeToExisting's AABB.ofSize(pos,1,1,1): centre within 0.75 on the
// flat, feet in (y-1, y+0.5).
func (o *Orb) WithinAwardMergeOf(x, y, z float64) bool {
	return o.X+orbHalfSize > x-0.5 && o.X-orbHalfSize < x+0.5 &&
		o.Y+orbHeight > y-0.5 && o.Y < y+0.5 &&
		o.Z+orbHalfSize > z-0.5 && o.Z-orbHalfSize < z+0.5
}

// orbBoxesTouch is scanForMerges' AABB.intersects: only a's box is grown by
// the merge distance, so centres merge up to 1.0 apart on an axis.
func orbBoxesTouch(a, b *Orb) bool {
	h, g := orbHalfSize, orbMergeDistance
	return a.X+h+g > b.X-h && a.X-h-g < b.X+h &&
		a.Y+orbHeight+g > b.Y && a.Y-g < b.Y+orbHeight &&
		a.Z+h+g > b.Z-h && a.Z-h-g < b.Z+h
}

// FallDistance is the fall banked since the body last stood on the ground.
func (b *Orb) FallDistance() float64 { return b.fallDistance }

// SetFallDistance replaces the banked fall.
func (b *Orb) SetFallDistance(d float64) { b.fallDistance = d }

// HurtDead applies a damage no source bypasses invulnerability for and
// reports whether the orb died.
func (o *Orb) HurtDead(damage float64) bool {
	_, dead := o.Hurt(damage, false)
	return dead
}
