package entity

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The thrown projectile in flight: egg, snowball, and splash and lingering
// potions, from the bytecode of ThrowableProjectile, Projectile,
// AbstractThrownPotion and the item classes that throw them. A plain value
// like the arrow, with bodies passed in and events returned.

// ThrowableProjectile's numbers: gravity 0.03 (getDefaultGravity), air drag
// (double)0.99f (applyInertia's whole-velocity scale; the widened float's
// exact double is what a tick reads), and water inertia 0.8 (the default
// Entity.getWaterInertia) instead of the air's in water. Potions use
// AbstractThrownPotion's getDefaultGravity of 0.05.
const (
	thrownGravity       = 0.03
	thrownPotionGravity = 0.05
	// thrownBottleGravity is the bottle o' enchanting's own pull.
	// vanilla: ThrownExperienceBottle.getDefaultGravity
	thrownBottleGravity = 0.07
	thrownAirDrag       = 0.9900000095367432
	thrownWaterInertia  = 0.8
	// windChargeHitDamage is the hurt a wind charge asks of the body it
	// strikes. vanilla: AbstractWindCharge.onHitEntity
	windChargeHitDamage = 1.0
)

// EntityType.THROWN_EGG, shared by snowball, ender pearl and potions:
// sized(0.25F, 0.25F), clientTrackingRange(4), updateInterval(10).
// ThrownSpawnBelow is the 0.1 the projectile constructor taking a
// LivingEntity takes off the thrower's eye height.
const (
	ThrownHalfSize   = 0.125
	ThrownHeight     = 0.25
	ThrownTracking   = 4
	ThrownInterval   = 10
	ThrownSpawnBelow = 0.1
)

// ThrownKind is which thrown projectile a body is.
type ThrownKind uint8

const (
	// ThrownEgg is the chicken's produce, whose break may leave chicks behind.
	ThrownEgg ThrownKind = iota
	// ThrownSnowball is the shovel's ball of snow, whose break leaves nothing.
	ThrownSnowball
	// ThrownEnderPearl teleports its thrower to where it lands.
	ThrownEnderPearl
	// ThrownSplashPotion shatters on impact and sprays its effects nearby.
	ThrownSplashPotion
	// ThrownLingeringPotion shatters into a cloud that keeps its effects up.
	ThrownLingeringPotion
	// ThrownWindCharge is the player's wind charge: straight and undragged,
	// bursting where it lands.
	ThrownWindCharge
	// ThrownBreezeWindCharge is the breeze's charge with a three-block burst.
	ThrownBreezeWindCharge
	// ThrownSmallFireball lights the fire where it lands and the body it hits.
	ThrownSmallFireball
	// ThrownLargeFireball explodes where it lands.
	ThrownLargeFireball
	// ThrownWitherSkull explodes where it lands and withers what it hits.
	ThrownWitherSkull
	// ThrownDragonFireball leaves an expanding instant-damage cloud.
	ThrownDragonFireball
	// ThrownExperienceBottle is the bottle o' enchanting: it shatters on
	// anything, hurting nothing, and leaves experience orbs.
	ThrownExperienceBottle
	// ThrownShulkerBullet is the shulker's homing bullet.
	ThrownShulkerBullet
	// ThrownLlamaSpit is the llama's spit: a hit hurts for 1 and the flight goes
	// on, a block or water ends it.
	ThrownLlamaSpit
)

// The AbstractHurtingProjectile numbers. The inertias are float fields in
// vanilla, so their widened values are what a tick scales by.
// vanilla: AbstractHurtingProjectile, WitherSkull.getInertia
const (
	FireballAcceleration     = 0.1
	fireballInertia          = float64(float32(0.95))
	fireballLiquidInertia    = float64(float32(0.8))
	dangerousSkullInertia    = float64(float32(0.73))
	SmallFireballHitDamage   = 5.0
	LargeFireballHitDamage   = 6.0
	WitherSkullOwnedDamage   = 8.0
	WitherSkullUnownedDamage = 5.0
)

// IsFireball reports an AbstractHurtingProjectile that accelerates along its
// heading: no gravity and no fixed throw speed.
func (k ThrownKind) IsFireball() bool {
	return k == ThrownSmallFireball || k == ThrownLargeFireball || k == ThrownWitherSkull || k == ThrownDragonFireball
}

// Burns reports a projectile that sets itself on fire each tick, so its hit
// primes TNT and lights campfires and candles.
// vanilla: AbstractHurtingProjectile.shouldBurn
func (k ThrownKind) Burns() bool { return k == ThrownSmallFireball || k == ThrownLargeFireball }

// IsWindCharge reports either AbstractWindCharge kind.
func (k ThrownKind) IsWindCharge() bool { return k == ThrownWindCharge || k == ThrownBreezeWindCharge }

// EntityType is the wire's entity type number of the kind, the type each
// Spawn Entity packet names the body by.
func (k ThrownKind) EntityType() int32 {
	switch k {
	case ThrownSnowball:
		return protocol.EntityTypeSnowball
	case ThrownEnderPearl:
		return protocol.EntityTypeEnderPearl
	case ThrownSplashPotion:
		return protocol.EntityTypeSplashPotion
	case ThrownLingeringPotion:
		return protocol.EntityTypeLingeringPotion
	case ThrownBreezeWindCharge:
		return protocol.EntityTypeBreezeWindCharge
	case ThrownWindCharge:
		return protocol.EntityTypeWindCharge
	case ThrownSmallFireball:
		return protocol.EntityTypeSmallFireball
	case ThrownLargeFireball:
		return protocol.EntityTypeFireball
	case ThrownDragonFireball:
		return protocol.EntityTypeDragonFireball
	case ThrownWitherSkull:
		return protocol.EntityTypeWitherSkull
	case ThrownExperienceBottle:
		return protocol.EntityTypeExperienceBottle
	case ThrownShulkerBullet:
		return protocol.EntityTypeShulkerBullet
	case ThrownLlamaSpit:
		return protocol.EntityTypeLlamaSpit
	}
	return protocol.EntityTypeEgg
}

// TrackingRange is the kind's client tracking range in chunks.
// vanilla: EntityTypes.SHULKER_BULLET clientTrackingRange(8)
func (k ThrownKind) TrackingRange() int {
	if k == ThrownShulkerBullet {
		return 8
	}
	return ThrownTracking
}

// UpdateInterval is the kind's movement update period in ticks.
// vanilla: EntityTypes.SHULKER_BULLET, the builder's default updateInterval 3
func (k ThrownKind) UpdateInterval() int {
	if k == ThrownShulkerBullet {
		return 3
	}
	return ThrownInterval
}

// halfWidth is half the side of the kind's box, whose height is the side.
// Only the bullet and the hurting kinds are sized apart from the quarter
// block.
// vanilla: EntityTypes.SHULKER_BULLET, SMALL_FIREBALL, WITHER_SKULL, WIND_CHARGE, BREEZE_WIND_CHARGE sized 0.3125; FIREBALL, DRAGON_FIREBALL sized 1.0
func (k ThrownKind) halfWidth() float64 {
	switch k {
	case ThrownShulkerBullet, ThrownSmallFireball, ThrownWitherSkull, ThrownWindCharge, ThrownBreezeWindCharge:
		return shulkerBulletHalf
	case ThrownLargeFireball, ThrownDragonFireball:
		return 0.5
	}
	return ThrownHalfSize
}

// throwable reports a ThrowableProjectile: egg, snowball, pearl, the
// potions and the experience bottle.
func (k ThrownKind) throwable() bool {
	switch k {
	case ThrownEgg, ThrownSnowball, ThrownEnderPearl, ThrownSplashPotion, ThrownLingeringPotion, ThrownExperienceBottle:
		return true
	}
	return false
}

// blockBox is the body's box at (x, y, z), the side its height too.
func (t *Thrown) blockBox(x, y, z float64) AABB {
	h := t.Kind.halfWidth()
	if t.Kind.IsWindCharge() {
		y -= windChargeBoxOffset
	}
	return AABB{x - h, y, z - h, x + h, y + 2*h, z + h}
}

// IsPotion reports the two thrown potions, which shatter: no entity event
// beside the world event, and the stack's contents applied.
func (k ThrownKind) IsPotion() bool {
	return k == ThrownSplashPotion || k == ThrownLingeringPotion
}

// Gravity is the kind's getDefaultGravity per tick: 0.03, 0.05 for potions,
// 0 for the wind charge.
func (k ThrownKind) Gravity() float64 {
	if k == ThrownShulkerBullet {
		return shulkerBulletGravity
	}
	if k == ThrownLlamaSpit {
		return llamaSpitGravity
	}
	if k.IsWindCharge() || k.IsFireball() {
		return 0
	}
	if k.IsPotion() {
		return thrownPotionGravity
	}
	if k == ThrownExperienceBottle {
		return thrownBottleGravity
	}
	return thrownGravity
}

// ThrownEventKind is something a thrown projectile's tick produced for the
// server.
type ThrownEventKind uint8

const (
	// ThrownHitTarget: the flight met a body before a block (onHitEntity). The
	// egg deals no damage, the snowball 3 to a blaze and none otherwise; the
	// hurt is called either way and the server reports the outcome.
	ThrownHitTarget ThrownEventKind = iota
	// ThrownBreak: the flight ended on a block face or a body (whose report is
	// the hit event). Either way onHit's tail follows: the server broadcasts
	// the hit's entity event and discards the body.
	ThrownBreak
	// ThrownDeflected: the flight met a body that turns projectiles back
	// (Target), and the body reversed off it with no hit; the server plays
	// a breeze's deflect sound.
	// vanilla: Projectile.hitTargetOrDeflectSelf
	ThrownDeflected
	// ThrownBubbleSurface: the body was pushed at the surface of the bubble
	// column cell X, Y, Z, where the server sends its splash.
	// vanilla: Projectile.onAboveBubbleColumn
	ThrownBubbleSurface
	// ThrownSplashed: the body first touched water at X, Y, Z; the server
	// plays entity.generic.splash at Volume and Pitch and posts a splash
	// game event. It comes ahead of the tick's other events.
	// vanilla: Entity.doWaterSplashEffect
	ThrownSplashed
	// ThrownBurnedUp: a shulker bullet met lava and was destroyed at X, Y, Z.
	// vanilla: LavaFluid.entityInside, ShulkerBullet.hurtServer
	ThrownBurnedUp
)

const (
	// thrownLavaFireTicks is Entity.lavaIgnite's 15 seconds.
	thrownLavaFireTicks = 300
	// thrownBurnFireTicks is AbstractHurtingProjectile.tick's igniteForSeconds(1).
	thrownBurnFireTicks = 20
)

// ThrownEvent is one event from a thrown projectile's tick. Target goes with
// ThrownHitTarget and a potion's or bottle's break on a body, Damage with
// ThrownHitTarget; X, Y, Z with both, where the flight ended
// (where chicks hatch and the break plays). Chicks goes with an egg's
// break (ThrownEgg.onHit's roll); BlockHit with a ThrownBreak on a block.
type ThrownEvent struct {
	Kind     ThrownEventKind
	Target   ArrowTarget
	Damage   float64
	X, Y, Z  float64
	Chicks   int
	BlockHit BlockHit
	// Volume and Pitch are a ThrownSplashed's sound.
	Volume, Pitch float32
}

// Thrown is one thrown projectile in a world. OwnerID is the thrower, whom
// it cannot hit in its first ticks (Projectile's leftOwner). Dead marks a
// discarded one.
type Thrown struct {
	ID     int32
	Silent bool
	Kind   ThrownKind
	Place
	VX, VY, VZ float64
	Yaw, Pitch float32
	// TicksFrozen is the powder snow count, up to rules.FullyFrozenTicks; only
	// lava takes it down.
	// vanilla: Entity.setTicksFrozen
	TicksFrozen int

	// TickCount is vanilla's tickCount, counted before the tick runs; the hit
	// margin and the tracker's send rhythm read it.
	TickCount int

	OwnerID int32
	Dead    bool
	// vanilla: Entity.isNoGravity
	NoGravity bool

	// Rand drives the body's rolls (above all the egg's chicks), seeded by the
	// caller so a replayed throw lands the same.
	Rand *rand.Rand

	// RandSrc is Rand's source when it was built by NewRolls, so a
	// held step can rewind the stream; nil otherwise.
	RandSrc *ReplaySource

	// AccelerationPower is how much of its heading a fireball adds to its
	// velocity each tick. vanilla: AbstractHurtingProjectile.accelerationPower
	AccelerationPower float64
	// ExplosionPower is a large fireball's blast radius.
	// vanilla: LargeFireball.explosionPower
	ExplosionPower int
	// Dangerous is a charged wither skull's flag.
	// vanilla: WitherSkull.isDangerous
	Dangerous bool
	// Bullet is a shulker bullet's flight state, nil on any other kind.
	Bullet *bulletFlight
	// noDeflect is the ticks a player's wind charge still refuses a
	// deflection; never saved. vanilla: WindCharge.noDeflectTicks
	noDeflect int
	// wasInWater is Entity.wasTouchingWater.
	wasInWater bool

	// FireTicks is vanilla's remainingFireTicks; FireFlag is the shared
	// on-fire flag, set from it by baseTick, so it trails an ignition by a
	// tick.
	FireTicks int
	FireFlag  bool

	// lastDeflectedBy is the body that last turned this one back, which
	// never turns it twice; 0 for none. Not saved.
	// vanilla: Projectile.lastDeflectedBy
	lastDeflectedBy int32

	leftOwner bool
	// Passed and HitAllowed are Arrow's: the bodies a ProjectileHitEvent
	// listener let the flight through, and the one it let land.
	Passed           []int32
	HitAllowed       int32
	leftOwnerChecked bool
}

// NewThrown places a thrown projectile at the position with the velocity,
// pointing along it (Projectile.shoot's yRot and xRot, whose yaw is atan2 of
// the velocity, not the walking convention).
func NewThrown(id int32, kind ThrownKind, x, y, z, vx, vy, vz float64, ownerID int32, seed int64) *Thrown {
	yaw, pitch := ArrowAngles(vx, vy, vz)
	rolls := NewRolls(seed)
	t := &Thrown{
		AccelerationPower: FireballAcceleration,
		ExplosionPower:    1,
		ID:                id,
		Kind:              kind,
		X:                 x, Y: y, Z: z,
		VX: vx, VY: vy, VZ: vz,
		Yaw:     yaw,
		Pitch:   pitch,
		OwnerID: ownerID,
		Rand:    rolls.Rand,
		RandSrc: rolls.Src,
	}
	if kind.IsWindCharge() {
		// vanilla: AbstractWindCharge.<init>
		t.AccelerationPower = 0
	}
	if kind == ThrownWindCharge {
		t.noDeflect = windChargeNoDeflectTicks
	}
	return t
}

// windChargeNoDeflectTicks is how long a fresh player's wind charge refuses
// a deflection. vanilla: WindCharge.noDeflectTicks
const windChargeNoDeflectTicks = 5

// thrownHitMargin is ProjectileUtil.computeMargin: 0 for the first two
// ticks, then 0.05 per tick past the second, capped at 0.3 (reached at age
// nine).
func thrownHitMargin(tickCount int) float64 {
	margin := float64(tickCount-2) / 20
	if margin < 0 {
		margin = 0
	}
	if margin > 0.3 {
		margin = 0.3
	}
	return margin
}

// Step advances the body one tick (ThrowableProjectile.tick) against the
// targets, with the thrower as owner (nil when gone): count the age, apply
// gravity and water or air drag, sweep the flight for bodies and clip it
// against blocks (the first decides), place the body at the end, ease its
// angles a fifth toward the velocity, and settle the thrower exclusion at
// the new position, in Projectile.tick's order.
func (t *Thrown) Step(w World, owner *ArrowTarget, targets []ArrowTarget) []ThrownEvent {
	return t.StepGather(w, func(*Thrown) (*ArrowTarget, []ArrowTarget) { return owner, targets })
}

// StepGather is Step with the thrower and targets asked for once the
// velocity of the move is settled, so a caller can query by MoveBox. A
// shulker bullet asks first, for its steering. gather is not called for a body
// that is dead or falls out of the world this tick.
func (t *Thrown) StepGather(w World, gather func(*Thrown) (*ArrowTarget, []ArrowTarget)) []ThrownEvent {
	if t.Dead {
		return nil
	}
	// vanilla: Entity.checkBelowWorld
	if t.Y < float64(w.MinY())-64 {
		t.Dead = true
		return nil
	}

	// vanilla: tickCount, counted by tickNonPassenger before the tick body,
	// so the hit margin reads this tick's age.
	t.TickCount++
	// Counted down ahead of the flight rather than after it as vanilla's
	// WindCharge.tick does: a deflection only ever comes between two ticks
	// of the charge's own.
	if t.noDeflect > 0 {
		t.noDeflect--
	}

	if t.Kind == ThrownLlamaSpit {
		owner, targets := gather(t)
		return t.stepSpit(w, owner, targets)
	}

	var bubbles []bubbleSpot
	pearl := t.Kind == ThrownEnderPearl
	// A throwable's first tick is pushed by every column its box is in
	// before it moves. vanilla: ThrowableProjectile.handleFirstTickBubbleColumn
	if t.TickCount == 1 && t.Kind.throwable() {
		box := t.blockBox(t.X, t.Y, t.Z)
		for x := int32(math.Floor(box.minX)); x <= int32(math.Floor(box.maxX)); x++ {
			for y := int32(math.Floor(box.minY)); y <= int32(math.Floor(box.maxY)); y++ {
				for z := int32(math.Floor(box.minZ)); z <= int32(math.Floor(box.maxZ)); z++ {
					if drag, ok := rules.BubbleColumnDrag(w.BlockAt(x, y, z)); ok {
						t.VY = projectileBubbleSpeed(w, x, y, z, drag, t.VY, pearl, &bubbles)
					}
				}
			}
		}
	}

	// A bullet's pull is the steering's own. Its baseTick runs ahead of the
	// flight (ShulkerBullet.tick's super.tick), the others' after the move.
	var bulletTarget *ArrowTarget
	var splash []ThrownEvent
	var owner *ArrowTarget
	var targets []ArrowTarget
	bullet := t.Kind == ThrownShulkerBullet
	if bullet {
		owner, targets = gather(t)
		splash = t.touchWater(w)
		bulletTarget = t.steerBullet(targets)
	} else if !t.NoGravity {
		// vanilla: applyGravity
		t.VY -= t.Kind.Gravity()
	}

	// Inertia reads the previous tick's post-move fluid check, initially dry.
	// vanilla: ThrowableProjectile.applyInertia, AbstractHurtingProjectile.applyInertia
	// A wind charge's inertia is 1 in both, so only a deflection's
	// acceleration changes its speed.
	// vanilla: AbstractWindCharge.getInertia
	if t.Kind.IsFireball() {
		t.accelerate(t.wasInWater)
	} else if t.Kind.IsWindCharge() {
		t.pushAlongHeading()
	} else if !bullet {
		if t.wasInWater {
			t.VX *= thrownWaterInertia
			t.VY *= thrownWaterInertia
			t.VZ *= thrownWaterInertia
		} else {
			t.VX *= thrownAirDrag
			t.VY *= thrownAirDrag
			t.VZ *= thrownAirDrag
		}
	}

	if !bullet {
		owner, targets = gather(t)
	}

	// The move: clip against blocks first (a block hit ends the flight),
	// then sweep for bodies; the first body along the line takes the tick.
	clip := clipProjectileFlight(w, t.X, t.Y, t.Z, t.VX, t.VY, t.VZ)
	blockHit := clip.ok
	endX, endY, endZ := clip.end(t.X, t.Y, t.Z, t.VX, t.VY, t.VZ)
	margin := thrownHitMargin(t.TickCount)
	ownerVehicle := int32(0)
	if owner != nil {
		ownerVehicle = owner.RootVehicle
	}
	hit, hitX, hitY, hitZ := sweepThrownTargets(t, targets, margin, ownerVehicle, t.X, t.Y, t.Z, endX, endY, endZ)
	fromX, fromY, fromZ := t.X, t.Y, t.Z
	t.SetPos(endX, endY, endZ)
	if hit != nil {
		t.SetPos(hitX, hitY, hitZ)
	}
	if !bullet {
		splash = t.touchWater(w)
	}

	// vanilla: updateRotation, a fifth of the way toward the velocity each
	// tick.
	if bullet && hit == nil && !blockHit {
		t.bulletMoved(w, bulletTarget)
	}
	t.Yaw = lerpRotation(t.Yaw, arrowTargetYaw(t.VX, t.VZ))
	t.Pitch = lerpRotation(t.Pitch, arrowTargetPitch(t.VY, t.VX, t.VZ))

	// The block effects of the move, after the turn; a column and the
	// freezing of powder snow are all a thrown body reacts to.
	// vanilla: ThrowableProjectile.tick, AbstractHurtingProjectile.tick
	moves := [1]bodyMove{{fx: fromX, fy: fromY, fz: fromZ, tx: t.X, ty: t.Y, tz: t.Z}}
	snow, inLava := false, false
	walkInside(w, moves[:], t.blockBox(0, 0, 0), func(x, y, z int32, state uint16, _ int, from AABB, dx, dy, dz float64) bool {
		t.VY, _ = projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, t.VY, pearl, &bubbles)
		if rules.IsPowderSnow(state) {
			snow = true
		} else if !inLava && !rules.IsFire(state) && sweptFluid(w, x, y, z, state, from, dx, dy, dz) == fluidLava {
			inLava = true
		}
		return true
	})
	applyFreeze(&t.TicksFrozen, snow, inLava)
	events := splash
	if inLava {
		if bullet {
			// vanilla: Entity.lavaHurt reaches ShulkerBullet.hurtServer
			t.Dead = true
			return append(events, ThrownEvent{Kind: ThrownBurnedUp, X: t.X, Y: t.Y, Z: t.Z})
		}
		t.FireTicks = max(t.FireTicks, thrownLavaFireTicks) // vanilla: Entity.lavaIgnite
	}
	// vanilla: Entity.baseTick, then AbstractHurtingProjectile.tick's ignite
	if t.FireTicks > 0 {
		t.FireTicks--
	}
	t.FireFlag = t.FireTicks > 0
	if t.Kind.Burns() {
		t.FireTicks = max(t.FireTicks, thrownBurnFireTicks)
	}
	for _, p := range bubbles {
		events = append(events, ThrownEvent{Kind: ThrownBubbleSurface, X: float64(p.x), Y: float64(p.y), Z: float64(p.z)})
	}

	// The thrower exclusion, settled at the moved position (Projectile.tick's
	// checkLeftOwner after the subclass's physics): once the grown flight box
	// stops touching the thrower or its vehicle group, they may be hit, for
	// good. The once-a-tick mark clears at the tick's end, so the check repeats
	// while the exclusion holds.
	if !t.leftOwner && !t.leftOwnerChecked {
		if owner == nil {
			t.leftOwner = true
		} else {
			box := t.thrownBox()
			t.leftOwner = !ownerGroupTouching(&box, owner, ownerVehicle, targets)
		}
		t.leftOwnerChecked = true
	}
	t.leftOwnerChecked = false

	return append(events, t.hitOutcome(hit, clip)...)
}

// hitOutcome ends a flight that met the body hit (nil for none) or the
// clipped block: a deflection, a hurt, a break, or nothing.
func (t *Thrown) hitOutcome(hit *ArrowTarget, clip flightClip) []ThrownEvent {
	if hit != nil && deflectsProjectile(hit, t.Kind.IsWindCharge()) {
		// The same body's second contact is no hit and no deflection.
		if hit.EntityID == t.lastDeflectedBy {
			return nil
		}
		t.lastDeflectedBy = hit.EntityID
		reverseDeflection(&t.VX, &t.VY, &t.VZ, &t.Yaw, t.Rand, 1)
		// vanilla: AbstractHurtingProjectile.onDeflection
		if t.Kind.IsFireball() {
			t.AccelerationPower *= 0.5
		}
		return []ThrownEvent{{Kind: ThrownDeflected, Target: *hit, X: t.X, Y: t.Y, Z: t.Z}}
	}
	if hit != nil {
		t.Dead = true
		// A potion shatters on a body as on a block, with no hurt: the potion
		// classes override no onHitEntity. The break event reports the shatter.
		// vanilla: AbstractThrownPotion.onHit, Projectile.onHitEntity
		if t.Kind.IsPotion() || t.Kind == ThrownExperienceBottle {
			return []ThrownEvent{{
				Kind:   ThrownBreak,
				Target: *hit,
				X:      t.X, Y: t.Y, Z: t.Z,
			}}
		}
		// vanilla: onHit after its super — hurt then break; the server applies
		// the hurt, hatches chicks and removes the body.
		return []ThrownEvent{{
			Kind:   ThrownHitTarget,
			Target: *hit,
			Damage: thrownHitDamage(t.Kind, hit.Type),
			X:      t.X, Y: t.Y, Z: t.Z,
			Chicks: t.chickRoll(),
		}}
	}
	if clip.ok {
		t.Dead = true
		return []ThrownEvent{{
			Kind: ThrownBreak,
			X:    t.X, Y: t.Y, Z: t.Z,
			Chicks: t.chickRoll(),
			BlockHit: BlockHit{
				X: clip.bx, Y: clip.by, Z: clip.bz,
				Face: clip.face,
				HitX: t.X, HitY: t.Y, HitZ: t.Z,
				State: clip.state, Border: clip.border,
			},
		}}
	}
	return nil
}

// touchWater sets wasInWater from the box at the body's position and returns
// the ThrownSplashed of a first touch after the body's first tick, else nil.
// vanilla: Entity.updateFluidInteraction
func (t *Thrown) touchWater(w World) []ThrownEvent {
	minX, minY, _, maxX, maxY, _ := t.BlastBox()
	in, _, _ := BoxInWater(w, t.X, minY, t.Z, maxX-minX, maxY-minY, minY)
	was := t.wasInWater
	t.wasInWater = in
	if !in || was || t.TickCount <= 1 {
		return nil
	}
	volume, pitch := splashSound(t.VX, t.VY, t.VZ, t.Rand)
	return []ThrownEvent{{Kind: ThrownSplashed, X: t.X, Y: t.Y, Z: t.Z, Volume: volume, Pitch: pitch}}
}

// accelerate adds the heading's push to the velocity, then scales by the
// medium's inertia.
// vanilla: AbstractHurtingProjectile.applyInertia
func (t *Thrown) accelerate(inWater bool) {
	inertia := fireballInertia
	switch {
	case inWater:
		inertia = fireballLiquidInertia
	case t.Kind == ThrownWitherSkull && t.Dangerous:
		inertia = dangerousSkullInertia
	}
	t.pushAlongHeading()
	t.VX *= inertia
	t.VY *= inertia
	t.VZ *= inertia
}

// pushAlongHeading adds AccelerationPower along the velocity's direction.
// vanilla: AbstractHurtingProjectile.applyInertia
func (t *Thrown) pushAlongHeading() {
	if t.AccelerationPower == 0 {
		return
	}
	length := math.Sqrt(t.VX*t.VX + t.VY*t.VY + t.VZ*t.VZ)
	// Vec3.normalize is zero below 1.0E-5.
	if length >= 1.0e-5 {
		t.VX += t.VX / length * t.AccelerationPower
		t.VY += t.VY / length * t.AccelerationPower
		t.VZ += t.VZ / length * t.AccelerationPower
	}
}

// thrownHitDamage is the kind's onHitEntity damage: none for egg and pearl;
// the snowball asks the rules what the target kind is worth.
func thrownHitDamage(kind ThrownKind, entityType int32) float64 {
	if kind.IsWindCharge() {
		return windChargeHitDamage
	}
	switch kind {
	case ThrownSmallFireball:
		return SmallFireballHitDamage
	case ThrownLargeFireball:
		return LargeFireballHitDamage
	case ThrownWitherSkull:
		return WitherSkullOwnedDamage
	case ThrownShulkerBullet:
		return ShulkerBulletDamage
	case ThrownLlamaSpit:
		return LlamaSpitDamage
	}
	if kind != ThrownSnowball {
		return 0
	}
	return rules.SnowballHitDamage(entityType)
}

// chickRoll is ThrownEgg.onHit's roll, whatever it hit: one chick on a 1 in
// 8, and four instead on a further 1 in 32 (asked only after the first
// passed). Snowballs roll nothing.
func (t *Thrown) chickRoll() int {
	if t.Kind != ThrownEgg || t.Rand == nil {
		return 0
	}
	if t.Rand.Intn(8) != 0 {
		return 0
	}
	n := 1
	if t.Rand.Intn(32) == 0 {
		n = 4
	}
	return n
}

// LeftOwner reports whether the body has cleared its thrower; saved with
// the player so a loaded pearl keeps it. Once set it stays set.
func (t *Thrown) LeftOwner() bool { return t.leftOwner }

// RestoreLeftOwner sets the thrower exclusion from a save.
func (t *Thrown) RestoreLeftOwner(left bool) { t.leftOwner = left }

// thrownBox is the body's box grown along the flight and by a block each
// way, for Projectile.isOutsideOwnerCollisionRange.
func (t *Thrown) thrownBox() ArrowTarget {
	return t.thrownBoxFor(t.VX, t.VY, t.VZ)
}

func (t *Thrown) thrownBoxFor(vx, vy, vz float64) ArrowTarget {
	grow := ThrownHalfSize + 1
	return ArrowTarget{
		MinX: math.Min(t.X, t.X+vx) - grow,
		MinY: math.Min(t.Y, t.Y+vy) - 1,
		MinZ: math.Min(t.Z, t.Z+vz) - grow,
		MaxX: math.Max(t.X, t.X+vx) + grow,
		MaxY: math.Max(t.Y, t.Y+vy) + ThrownHeight + 1,
		MaxZ: math.Max(t.Z, t.Z+vz) + grow,
	}
}

// MoveBox is the box (x, y, z, x, y, z) of what the move may meet: the
// kind's own box swept over the velocity StepGather has settled, grown by a
// block. A shulker bullet's steering is not modelled; callers pad it.
// vanilla: ProjectileUtil.getEntityHitResult
func (t *Thrown) MoveBox() [6]float64 {
	b := t.blockBox(t.X, t.Y, t.Z)
	return [6]float64{
		math.Min(b.minX, b.minX+t.VX) - 1, math.Min(b.minY, b.minY+t.VY) - 1, math.Min(b.minZ, b.minZ+t.VZ) - 1,
		math.Max(b.maxX, b.maxX+t.VX) + 1, math.Max(b.maxY, b.maxY+t.VY) + 1, math.Max(b.maxZ, b.maxZ+t.VZ) + 1,
	}
}

// sweepThrownTargets returns the body whose box (grown by the age's margin)
// the flight line enters first, and the entry point (an EntityHitResult's
// location). A box already containing the start is passed through. The
// thrower and its vehicle group are skipped while the flight is among them
// (canHitEntity).
// vanilla: ProjectileUtil.getEntityHitResult, AABB.clip
func sweepThrownTargets(t *Thrown, targets []ArrowTarget, margin float64, ownerVehicle int32, fromX, fromY, fromZ, endX, endY, endZ float64) (*ArrowTarget, float64, float64, float64) {
	dx, dy, dz := endX-fromX, endY-fromY, endZ-fromZ
	var best *ArrowTarget
	bestT := math.Inf(1)
	var hx, hy, hz float64
	for i := range targets {
		target := &targets[i]
		if target.Unhittable || t.Kind == ThrownDragonFireball && (target.EntityID == t.OwnerID || MobType(target.Type) == TypeEnderDragon && target.EntityID > t.OwnerID && target.EntityID <= t.OwnerID+8) {
			continue // vanilla: canBeHitByProjectile, the spectator's no
		}
		if ownerExcluded(t.OwnerID, ownerVehicle, t.leftOwner, target) || slices.Contains(t.Passed, target.EntityID) {
			continue
		}
		enter, _, okT := world.SegmentAABB(fromX, fromY, fromZ, dx, dy, dz,
			target.MinX-margin, target.MinY-margin, target.MinZ-margin,
			target.MaxX+margin, target.MaxY+margin, target.MaxZ+margin)
		if !okT || enter <= 0 || enter >= bestT {
			continue
		}
		best, bestT = target, enter
		hx, hy, hz = fromX+dx*enter, fromY+dy*enter, fromZ+dz*enter
	}
	return best, hx, hy, hz
}

// Splash reach: candidates within the potion box grown 4 by 2 by 4, then
// within squared distance 16 (past the hit margin) at strength
// 1 - sqrt(dist)/4.
// vanilla: AbstractThrownPotion.SPLASH_RANGE and ThrownSplashPotion.onHitAsPotion
const (
	splashReach = 4.0
	splashRise  = 2.0
)

// SplashHit is a body a splash reached and its strength: 1 at the shatter,
// falling to 0 four blocks out.
type SplashHit struct {
	Target ArrowTarget
	Scale  float64
}

// splashBox is the potion's quarter-block box at the shatter point, which
// both splash and water reach grow from.
func splashBox(x, y, z float64) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return x - ThrownHalfSize, y, z - ThrownHalfSize,
		x + ThrownHalfSize, y + ThrownHeight, z + ThrownHalfSize
}

// SplashHits is the bodies a shatter at (x, y, z) reaches: candidates from
// the potion box grown 4 by 2 by 4, each measured box to box against the
// ungrown one with the hit margin on the candidate's box. Within squared
// distance 16 the strength is 1 - sqrt(dist)/4.
// vanilla: ThrownSplashPotion.onHitAsPotion
func (t *Thrown) SplashHits(x, y, z float64, targets []ArrowTarget) []SplashHit {
	margin := thrownHitMargin(t.TickCount)
	minX, minY, minZ, maxX, maxY, maxZ := splashBox(x, y, z)
	var hits []SplashHit
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: getEntitiesOfClass's NO_SPECTATORS
		}
		// The grown box picks candidates with AABB's strict intersect (touching
		// is not reaching). The margin rides along, so only the ungrown box
		// gates.
		if target.MinX-margin >= maxX+splashReach || target.MaxX+margin <= minX-splashReach ||
			target.MinY-margin >= maxY+splashRise || target.MaxY+margin <= minY-splashRise ||
			target.MinZ-margin >= maxZ+splashReach || target.MaxZ+margin <= minZ-splashReach {
			continue
		}
		dist := boxesDistanceSq(minX, minY, minZ, maxX, maxY, maxZ,
			target.MinX-margin, target.MinY-margin, target.MinZ-margin,
			target.MaxX+margin, target.MaxY+margin, target.MaxZ+margin)
		if dist >= splashReach*splashReach {
			continue
		}
		hits = append(hits, SplashHit{Target: *target, Scale: 1 - math.Sqrt(dist)/splashReach})
	}
	return hits
}

// WaterHits is the bodies a water splash at (x, y, z) reaches: candidates
// from the potion box grown 4 by 2 by 4, then squared distance 16 from the
// potion's position to each body's position (not box to box). It is what
// onHitAsWater douses; no effect potion reads it.
// vanilla: AbstractThrownPotion.onHitAsWater
func (t *Thrown) WaterHits(x, y, z float64, targets []ArrowTarget) []ArrowTarget {
	minX, minY, minZ, maxX, maxY, maxZ := splashBox(x, y, z)
	minX, minY, minZ = minX-splashReach, minY-splashRise, minZ-splashReach
	maxX, maxY, maxZ = maxX+splashReach, maxY+splashRise, maxZ+splashReach
	var hits []ArrowTarget
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: getEntitiesOfClass's NO_SPECTATORS
		}
		// AABB's strict intersect: touching is not reaching.
		if target.MinX >= maxX || target.MaxX <= minX ||
			target.MinY >= maxY || target.MaxY <= minY ||
			target.MinZ >= maxZ || target.MaxZ <= minZ {
			continue
		}
		// The body's position (box centre, floor y) from the potion's: centre to
		// feet.
		cx := (target.MinX + target.MaxX) / 2
		cz := (target.MinZ + target.MaxZ) / 2
		dx, dy, dz := cx-x, target.MinY-y, cz-z
		if dx*dx+dy*dy+dz*dz >= splashReach*splashReach {
			continue
		}
		hits = append(hits, *target)
	}
	return hits
}

// boxesDistanceSq is the squared distance between boxes, zero when they
// overlap (each axis's gap floored at 0).
// vanilla: AABB.distanceToSqr
func boxesDistanceSq(minX1, minY1, minZ1, maxX1, maxY1, maxZ1,
	minX2, minY2, minZ2, maxX2, maxY2, maxZ2 float64) float64 {
	dx := math.Max(math.Max(minX2-maxX1, minX1-maxX2), 0)
	dy := math.Max(math.Max(minY2-maxY1, minY1-maxY2), 0)
	dz := math.Max(math.Max(minZ2-maxZ1, minZ1-maxZ2), 0)
	return dx*dx + dy*dy + dz*dz
}

// BlastBox is the kind's own box: what ServerExplosion.getSeenPercent
// samples, and where a blow, a shaft, a shell's push or another projectile's
// flight meets the body.
func (t *Thrown) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	b := t.blockBox(t.X, t.Y, t.Z)
	return b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ
}

// windChargeBoxOffset is how far below its position a wind charge's box sits.
// vanilla: AbstractWindCharge.makeBoundingBox
const windChargeBoxOffset = 0.15

// EyePosition is getEyePosition, where an explosion's knockback is directed
// from.
func (t *Thrown) EyePosition() (x, y, z float64) {
	return t.X, t.Y + t.Kind.eyeHeight(), t.Z
}

// eyeHeight is the kind's eye above its position: the builder's default,
// 0.85 of the height in float32, except for the wind charges' 0.
// vanilla: EntityDimensions.defaultEyeHeight, EntityTypes.WIND_CHARGE, BREEZE_WIND_CHARGE eyeHeight(0.0f)
func (k ThrownKind) eyeHeight() float64 {
	if k.IsWindCharge() {
		return 0
	}
	return float64(float32(2*k.halfWidth()) * 0.85)
}

// Push is Entity.push for an explosion's knockback; the motion check sees it
// next tick.
func (t *Thrown) Push(vx, vy, vz float64) {
	t.VX += vx
	t.VY += vy
	t.VZ += vz
}

// Redirectable reports whether a blow or a projectile's hit turns the body
// aside, and whether a projectile's flight meets it at all.
// vanilla: EntityTypeTags.REDIRECTABLE_PROJECTILE, Projectile.isPickable
func (t *Thrown) Redirectable() bool {
	return registry.EntityType(t.Kind.EntityType()).InTag(registry.EntityTypeTagIDRedirectableProjectile)
}

// ProjectilePower is the acceleration a hurting projectile's client
// predicts with.
// vanilla: ClientboundProjectilePowerPacket
func (t *Thrown) ProjectilePower() float64 {
	return t.AccelerationPower
}

// CanHitProjectile reports whether the body's flight may meet p, a shulker
// bullet or a redirectable body, this tick: never itself, a bullet no
// bullet, a wind charge no wind charge, and only within the flight's reach.
// vanilla: AbstractWindCharge.canHitEntity
func (t *Thrown) CanHitProjectile(p ArrowTarget) bool {
	switch {
	case p.EntityID == t.ID:
		return false
	case p.Type == protocol.EntityTypeShulkerBullet && t.Kind == ThrownShulkerBullet:
		return false
	case (p.Type == protocol.EntityTypeWindCharge || p.Type == protocol.EntityTypeBreezeWindCharge) && t.Kind.IsWindCharge():
		return false
	}
	// The step's gravity, drag, push or a bullet's turn moves the flight's
	// end by less than twice the speed and a half; the rest of the 2 covers
	// the body's half-size and the age margin.
	grow := 2*math.Sqrt(t.VX*t.VX+t.VY*t.VY+t.VZ*t.VZ) + 2
	return p.MaxX >= math.Min(t.X, t.X+t.VX)-grow && p.MinX <= math.Max(t.X, t.X+t.VX)+grow &&
		p.MaxY >= math.Min(t.Y, t.Y+t.VY)-grow && p.MinY <= math.Max(t.Y, t.Y+t.VY)+grow &&
		p.MaxZ >= math.Min(t.Z, t.Z+t.VZ)-grow && p.MinZ <= math.Max(t.Z, t.Z+t.VZ)+grow
}

// Deflect sends the body along the look (aimed false keeps its motion: the
// deflector is gone) and makes ownerID its owner, reporting whether it
// turned: a fresh player's wind charge refuses. Every deflection here is
// by attack, which sets the acceleration to 0.1.
// vanilla: Projectile.deflect, WindCharge.deflect, ProjectileDeflection.AIM_DEFLECT, AbstractHurtingProjectile.onDeflection
func (t *Thrown) Deflect(lx, ly, lz float64, aimed bool, ownerID int32) bool {
	if t.noDeflect > 0 {
		return false
	}
	if aimed {
		t.VX, t.VY, t.VZ = lx, ly, lz
	}
	t.OwnerID = ownerID
	t.AccelerationPower = FireballAcceleration
	return true
}

// SmallFireballBurnTicks is the 5 seconds a small fireball sets its victim
// alight for.
// vanilla: SmallFireball.onHitEntity
const SmallFireballBurnTicks = 100

// WitherSkullEffectSeconds is the Wither a skull inflicts by difficulty:
// none on easy and peaceful.
// vanilla: WitherSkull.onHitEntity
func WitherSkullEffectSeconds(difficulty uint8) int32 {
	switch difficulty {
	case rules.DifficultyNormal:
		return 10
	case rules.DifficultyHard:
		return 40
	}
	return 0
}

// FireballDamage is the blow a fireball of the kind deals and its damage
// type. A skull with no living owner deals magic damage.
// vanilla: SmallFireball.onHitEntity, LargeFireball.onHitEntity, WitherSkull.onHitEntity
func FireballDamage(kind ThrownKind, hasOwner, ownerLiving bool) (float64, registry.DamageType) {
	if kind == ThrownWitherSkull {
		if ownerLiving {
			return WitherSkullOwnedDamage, rules.DamageTypeWitherSkull
		}
		return WitherSkullUnownedDamage, rules.DamageTypeMagic
	}
	damage := SmallFireballHitDamage
	if kind == ThrownLargeFireball {
		damage = LargeFireballHitDamage
	}
	if hasOwner {
		return damage, rules.DamageTypeFireball
	}
	return damage, rules.DamageTypeUnattributedFireball
}

// WitherSkullKillHeal is the health a skull's owner regains when the blow
// kills its target. vanilla: WitherSkull.onHitEntity
const WitherSkullKillHeal = 5.0
