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

// The arrow in flight, from the bytecode of Projectile, AbstractArrow
// and Arrow. Like the item entities it is a plain value: the world is read
// through this package's interface, hittable bodies come in as a list, and
// anything to tell comes back as events.

// AbstractArrow's numbers: gravity 0.05 (getDefaultGravity), air drag 0.99
// (getAirDrag), water inertia 0.6 (getWaterInertia, instead of the air's);
// base damage 2.0; a stuck arrow despawns at 1200 (tickDespawn); a stick
// winds a shake of 7 (onSyncedDataUpdated, run on both sides when the
// in-ground flag turns); and onHitBlock draws the hit point back along the
// velocity's signs by 0.05000000074505806.
const (
	arrowGravity      = 0.05
	arrowAirDrag      = 0.99
	arrowWaterInertia = 0.6
	ArrowBaseDamage   = 2.0
	arrowLifeLimit    = 1200
	arrowShakeTicks   = 7
	arrowStuckOffset  = 0.05000000074505806

	// arrowPotionDecay is Arrow.EXPOSED_POTION_DECAY_TIME: a tipped arrow
	// this long in the ground turns plain.
	arrowPotionDecay = 600

	// arrowFallInflate is AbstractArrow.shouldFall's 0.06, growing the position
	// into the 0.12 cube the level is asked about.
	arrowFallInflate = 0.06

	// ArrowNoLastState is lastState for an arrow that never hit a block
	// (vanilla's null): no state has it, so a loaded in-ground arrow with no
	// inBlockState differs from any state at its cell, air included.
	ArrowNoLastState = ^uint16(0)
)

// ArrowEffect is a tipped arrow's status effects from its potion contents;
// the zero value is a plain arrow.
type ArrowEffect struct {
	// Tipped is set when the potion contents are not empty, even with no
	// effect: the arrow shows the potion colour and decays.
	Tipped bool
	// Color is the potion contents' custom_color, raw, when HasColor.
	Color     int32
	HasColor  bool
	Name      string
	Ticks     int32
	Amplifier int32
	// More are the effects after the first, in application order.
	More []ArrowStatus
}

// ArrowStatus is one status effect of a tipped shot beyond the first.
type ArrowStatus struct {
	Name      string
	Ticks     int32
	Amplifier int32
}

// IsPlain reports whether the effect is the zero value, a plain arrow.
func (e ArrowEffect) IsPlain() bool {
	return !e.Tipped && !e.HasColor && e.Color == 0 && e.Name == "" && e.Ticks == 0 && e.Amplifier == 0 && len(e.More) == 0
}

// EntityTypes.ARROW: sized(0.5F, 0.5F) with the position at the bottom
// centre, clientTrackingRange(4), updateInterval(20). ArrowSpawnBelow is the
// 0.1 the AbstractArrow(LivingEntity) constructor takes off the shooter's
// eye height.
const (
	ArrowHalfSize   = 0.25
	ArrowHeight     = 0.5
	ArrowTracking   = 4
	ArrowInterval   = 20
	ArrowSpawnBelow = 0.1
)

// ArrowPickup is AbstractArrow$Pickup, who may pick up a stuck arrow. The
// creative-only value is for arrows a creative player shoots from nothing
// (ProjectileWeaponItem.useAmmo's creative branch sets the ammo's
// intangible-projectile component).
type ArrowPickup uint8

const (
	PickupDisallowed   ArrowPickup = iota // AbstractArrow$Pickup.DISALLOWED
	PickupAllowed                         // AbstractArrow$Pickup.ALLOWED
	PickupCreativeOnly                    // AbstractArrow$Pickup.CREATIVE_ONLY
)

// ArrowTarget is a body an arrow may hit: its box, its entity ID, and
// whether projectiles pass through it (a spectator). Type is the
// minecraft:entity_type ID, which weapon enchantment conditions ask about.
// The owner is passed in this shape too, for the owner exclusion and the
// eye height a loyalty return aims at.
// vanilla: Entity.canBeHitByProjectile
type ArrowTarget struct {
	EntityID                           int32
	Type                               int32
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64
	Unhittable                         bool
	// RootVehicle is the entity ID of the body's root vehicle, 0 when riding
	// nothing. A projectile cannot hit the shooter's vehicle or a fellow
	// passenger until it has left that group.
	// vanilla: Entity.getRootVehicle, Projectile.canHitEntity
	RootVehicle int32
	// EyeY is the body's eye height this tick, which a loyalty trident's return
	// aims at; nothing else reads it.
	EyeY float64
}

// MobTarget is a mob as an arrow target: its BlastBox, its entity type ID,
// and its eye height.
func MobTarget(id, entityType int32, m *Mob) ArrowTarget {
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	return ArrowTarget{
		EntityID: id,
		Type:     entityType,
		MinX:     minX, MinY: minY, MinZ: minZ,
		MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		EyeY:       m.Y + m.eyeHeight(),
		Unhittable: m.Type == TypeEnderDragon,
	}
}

// Box is the projectile target's uninflated hitbox.
func (t *ArrowTarget) Box() AABB {
	return AABB{t.MinX, t.MinY, t.MinZ, t.MaxX, t.MaxY, t.MaxZ}
}

// PlayerTarget is a player as an arrow target: the standing 0.6 by 1.8 box,
// the player entity type ID, and standing eye height. unhittable marks a
// spectator.
// vanilla: Entity.canBeHitByProjectile
func PlayerTarget(id int32, x, y, z float64, unhittable bool) ArrowTarget {
	return SizedPlayerTarget(id, x, y, z, PlayerWidth, PlayerHeight, playerEyeHeight, unhittable)
}

// SizedPlayerTarget is PlayerTarget for a player of the given box, which its
// pose and scale set.
// vanilla: Entity.getBoundingBox
func SizedPlayerTarget(id int32, x, y, z, width, height, eye float64, unhittable bool) ArrowTarget {
	hw := width / 2
	return ArrowTarget{
		EntityID: id,
		Type:     protocol.EntityTypePlayer,
		MinX:     x - hw, MinY: y, MinZ: z - hw,
		MaxX: x + hw, MaxY: y + height, MaxZ: z + hw,
		Unhittable: unhittable,
		EyeY:       y + eye,
	}
}

// ArrowEventKind is something an arrow's tick produced for the server.
type ArrowEventKind uint8

const (
	// ArrowHitTarget: the flight met a body before a block (onHitEntity). Damage
	// is the base damage times speed, ceiled, plus the critical roll; the
	// server's pipeline decides the hurt.
	ArrowHitTarget ArrowEventKind = iota
	// ArrowStuck: the flight met a block first (onHitBlock); the server plays
	// the hit sound.
	ArrowStuck
	// ArrowPickedUp: a player touched the stuck arrow (playerTouch) and gains
	// one plain arrow (getPickupItem).
	ArrowPickedUp
	// ArrowGone: a stuck arrow's life ran out (tickDespawn).
	ArrowGone
	// ArrowTridentReturn: a loyalty trident started returning from X, Y, Z;
	// the server plays the return sound at volume 10.
	ArrowTridentReturn
	// ArrowEffectFaded: a tipped arrow in the ground lost its effect. Entity
	// event 0 goes out before the colour metadata resets, since the client
	// draws the puff in that colour.
	ArrowEffectFaded
	// ArrowDousedCauldron: a burning arrow was extinguished in the layered
	// cauldron at X, Y, Z, which the server lowers a level.
	// vanilla: LayeredCauldronBlock.entityInside
	ArrowDousedCauldron
	// ArrowMeltedSnow: a burning arrow was extinguished in the powder snow
	// cell at X, Y, Z, which the server destroys when mob_griefing allows.
	// vanilla: PowderSnowBlock.entityInside
	ArrowMeltedSnow
	// ArrowSnowflake: the arrow flew sideways through the powder snow cell
	// whose top is Y, and may kick up a snowflake there at X, Z. The flight
	// never runs Entity.move, so the snow does not slow it.
	// vanilla: PowderSnowBlock.entityInside, AbstractArrow.tick
	ArrowSnowflake
	// ArrowDeflected: the flight reversed without a hit; Target is the
	// deflecting body, or zero for a world border bounce.
	// vanilla: Projectile.hitTargetOrDeflectSelf
	ArrowDeflected
	// ArrowBubbleSurface: the arrow was pushed at the surface of the bubble
	// column cell X, Y, Z, where the server sends its splash.
	// vanilla: Projectile.onAboveBubbleColumn
	ArrowBubbleSurface
	// ArrowSplashed: the arrow first touched water at X, Y, Z; the server
	// plays entity.generic.splash at Volume and Pitch and posts a splash
	// game event.
	// vanilla: Entity.doWaterSplashEffect
	ArrowSplashed
)

// BlockHit is a projectile's BlockHitResult: the cell, the state, the entry
// face (a protocol.Face*) and the crossing point on that face.
type BlockHit struct {
	X, Y, Z          int32
	Face             int32
	HitX, HitY, HitZ float64
	State            uint16
	// Border marks a world border hit, which must skip per-block hooks.
	Border bool
	// Speed is the velocity length at the hit in blocks per tick; zero when not
	// reported.
	Speed float64
}

// ArrowEvent is one event from an arrow's tick. Target, Damage and
// IgniteTicks go with ArrowHitTarget; X, Y, Z with ArrowStuck or
// ArrowTridentReturn; Collector with ArrowPickedUp; BlockHit with ArrowStuck.
type ArrowEvent struct {
	Kind    ArrowEventKind
	Target  ArrowTarget
	Damage  float64
	X, Y, Z float64
	// VX, VY and VZ are the flight at a body impact, before a later block
	// landing or deflection changes the shaft's velocity.
	VX, VY, VZ float64
	Collector  int32
	// BlockHit is the block met, its entry face and crossing point, distinct
	// from X, Y, Z (the drawn-back resting place). Only on ArrowStuck, for the
	// blocks that react to a projectile hit.
	BlockHit BlockHit
	// IgniteTicks is the burn the hit gives the body: 100 ticks from a burning
	// arrow, else 0. Vanilla ignites before the hurt, and the caller keeps that
	// order.
	IgniteTicks int32
	// HitSound is the selected sound at an entity or block impact, before a
	// block landing can reset the arrow's sound.
	HitSound registry.Sound
	// Volume and Pitch are an ArrowSplashed's sound.
	Volume, Pitch float32
}

// Arrow is one arrow in a world. OwnerID is the shooter, whom the arrow
// cannot hit in its first ticks (Projectile's leftOwner rule). Dead marks a
// discarded arrow: hit, picked up or despawned.
type Arrow struct {
	ID int32
	Place
	VX, VY, VZ float64
	Yaw, Pitch float32

	InGround   bool
	Crit       bool
	ShakeTicks int
	Life       int
	BaseDamage float64
	Pickup     ArrowPickup
	OwnerID    int32
	Dead       bool
	// Passed is the bodies a ProjectileHitEvent listener let the flight go
	// through; HitAllowed is the one body whose hit a listener let land, spent
	// by the next step.
	Passed     []int32
	HitAllowed int32
	// SparePlayers makes the sweep skip every player: no hit, ignite or
	// deflection. The caller sets it before each tick.
	// vanilla: AbstractArrow.canHitEntity
	SparePlayers bool
	// SpareIDs is the player entity IDs the owner may not harm through its
	// team, skipped the same way. The caller sets it before each tick.
	// vanilla: AbstractArrow.canHitEntity, Player.canHarmPlayer
	SpareIDs []int32
	// vanilla: Entity.isNoGravity
	NoGravity bool

	// LastState is vanilla's lastState, the state of the block last hit: the
	// arrow falls once its cell's state differs and nothing holds its point
	// box. ArrowNoLastState is vanilla's null.
	LastState uint16

	// Weapon is firedFromWeapon: a player's bow or a skeleton's.
	Weapon protocol.ItemStack
	// BindLoot fills the bodies a data-pack effect's loot conditions read
	// into a hit's damage context; nil leaves it type-only. The server sets
	// it for each step.
	BindLoot func(ctx *rules.EnchContext, target *ArrowTarget)

	// PickupStack is the stack a pickup returns when not the plain arrow; empty
	// means the arrow type decides.
	// vanilla: AbstractArrow.getPickupItem
	PickupStack protocol.ItemStack
	// FireTicks is remainingFireTicks (read through isOnFire), saved as the
	// base entity's Fire.
	FireTicks int
	// TicksFrozen is the powder snow count, up to rules.FullyFrozenTicks; only
	// lava and fire take it down.
	// vanilla: Entity.setTicksFrozen
	TicksFrozen int
	// Effect is what the shot gives the body it hits.
	Effect ArrowEffect
	// GlowTicks is a spectral arrow's SpectralArrow.duration.
	GlowTicks int32

	// Trident marks a ThrownTrident: its hit deals a fixed 8 and leaves the
	// body in the world, Weapon is both the damage enchantments' source and the
	// pickup, and a loyalty trident returns to its owner once spent. Flight,
	// sticking and shake are the arrow's.
	Trident bool
	// Loyalty is the thrown stack's loyalty level, synced and setting the
	// return's pace; 0 without it and for every arrow.
	Loyalty int32
	// Channeling gates the thrown weapon's lightning effects.
	Channeling int32
	// Foil is a trident's synced glint, the thrown stack's, set by a throw
	// alone: a summoned or saved trident has none.
	// vanilla: ThrownTrident(Level, LivingEntity, ItemStack), readAdditionalSaveData
	Foil bool
	// NoPhysics is Entity.setNoPhysics, set by the loyalty return: sent as the
	// arrow flags' second bit, and what lets the return reach the owner
	// anywhere.
	NoPhysics bool

	// PierceLevel is how many bodies past the first the arrow passes through;
	// pierced bodies are skipped afterwards.
	// vanilla: AbstractArrow.getPierceLevel
	PierceLevel int32

	// HitSound is the registered impact sound, reset to entity.arrow.hit on a block.
	// vanilla: AbstractArrow.soundEvent, onHitBlock
	HitSound registry.Sound

	// Rand drives the arrow's rolls (above all the critical bonus), seeded by
	// the caller so a replayed shot lands the same.
	Rand *rand.Rand

	// RandSrc is Rand's source when it was built by NewRolls, so a
	// held step can rewind the stream; nil otherwise.
	RandSrc *ReplaySource

	// lastDeflectedBy is the body that last turned the arrow back, which
	// never turns it twice; 0 for none. Not saved.
	// vanilla: Projectile.lastDeflectedBy
	lastDeflectedBy int32

	// dealtDamage is ThrownTrident.dealtDamage: the trident hit a body or lay
	// stuck past ThrownTrident.tick's five ticks; from then the sweep skips all
	// bodies and only the loyalty return applies. Saved.
	dealtDamage      bool
	pierced          []int32
	leftOwner        bool
	leftOwnerChecked bool
	// inWater is Entity.wasTouchingWater: the box touched water where the
	// last flight tick ended (baseTick). It starts false, so a fresh arrow's
	// first tick gets air drag even underwater, and in-ground ticks leave it.
	inWater bool
	// ticked is !Entity.firstTick: a step has finished.
	ticked bool
	// tickCount saturates once ProjectileUtil.computeMargin reaches its cap.
	tickCount int
	// splash is the ArrowSplashed the step's water test recorded, nil for
	// none.
	splash *ArrowEvent
}

// NewArrow places an arrow at the position with the velocity, pointing along
// it (Projectile.shoot's yRot/xRot). pickup says who may take it back, the
// owner is the shooter it cannot hit at first, and seed makes its rolls
// reproducible.
func NewArrow(id int32, x, y, z, vx, vy, vz float64, crit bool, baseDamage float64, pickup ArrowPickup, ownerID int32, seed int64) *Arrow {
	yaw, pitch := ArrowAngles(vx, vy, vz)
	rolls := NewRolls(seed)
	return &Arrow{
		ID: id,
		X:  x, Y: y, Z: z,
		VX: vx, VY: vy, VZ: vz,
		Yaw:        yaw,
		Pitch:      pitch,
		Crit:       crit,
		BaseDamage: baseDamage,
		Pickup:     pickup,
		OwnerID:    ownerID,
		LastState:  ArrowNoLastState,
		HitSound:   registry.SoundEntityArrowHit,
		Rand:       rolls.Rand,
		RandSrc:    rolls.Src,
	}
}

// ArrowAngles is Projectile.shoot's yaw and pitch in degrees for a velocity:
// yaw atan2 of x and z (mirrored against the walking convention), pitch
// atan2 of the rise over horizontal distance.
func ArrowAngles(vx, vy, vz float64) (yaw, pitch float32) {
	horizontal := math.Hypot(vx, vz)
	return float32(math.Atan2(vx, vz) * 180 / math.Pi),
		float32(math.Atan2(vy, horizontal) * 180 / math.Pi)
}

// ShootVelocity is Projectile.getMovementToShoot: the normalised aim plus
// triangular noise (RandomSource.triangle, mode 0, deviation 0.0172275 ×
// inaccuracy) on each axis, scaled by power.
func ShootVelocity(aimX, aimY, aimZ, power, inaccuracy float64, rng *rand.Rand) (vx, vy, vz float64) {
	length := math.Sqrt(aimX*aimX + aimY*aimY + aimZ*aimZ)
	if length < 1e-9 {
		return 0, 0, 0
	}
	aimX, aimY, aimZ = aimX/length, aimY/length, aimZ/length
	dev := 0.0172275 * inaccuracy
	aimX += dev * (rng.Float64() - rng.Float64())
	aimY += dev * (rng.Float64() - rng.Float64())
	aimZ += dev * (rng.Float64() - rng.Float64())
	return aimX * power, aimY * power, aimZ * power
}

// MobArrowDamage is setBaseDamageFromMob as ProjectileUtil.getMobArrow calls
// it: twice the charge plus a triangular roll of mode 0.11 × difficulty and
// deviation 0.57425.
func MobArrowDamage(charge float64, difficulty uint8, rng *rand.Rand) float64 {
	return charge*2 + 0.11*float64(difficulty) + 0.57425*(rng.Float64()-rng.Float64())
}

// boxesTouch reports whether the two boxes overlap at all.
func boxesTouch(a, b *ArrowTarget) bool {
	return a.MinX <= b.MaxX && a.MaxX >= b.MinX &&
		a.MinY <= b.MaxY && a.MaxY >= b.MinY &&
		a.MinZ <= b.MaxZ && a.MaxZ >= b.MinZ
}

// arrowBox is the arrow's box grown along the flight and by a block each
// way, the shape Projectile.isOutsideOwnerCollisionRange tests.
func (a *Arrow) arrowBox() ArrowTarget {
	return ArrowTarget{
		MinX: math.Min(a.X, a.X+a.VX) - ArrowHalfSize - 1,
		MinY: math.Min(a.Y, a.Y+a.VY),
		MinZ: math.Min(a.Z, a.Z+a.VZ) - ArrowHalfSize - 1,
		MaxX: math.Max(a.X, a.X+a.VX) + ArrowHalfSize + 1,
		MaxY: math.Max(a.Y, a.Y+a.VY) + ArrowHeight,
		MaxZ: math.Max(a.Z, a.Z+a.VZ) + ArrowHalfSize + 1,
	}
}

// SweptBox is arrowBox's bounds, so the caller can pass only bodies the
// tick's flight could reach.
func (a *Arrow) SweptBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	b := a.arrowBox()
	return b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ
}

// Step advances the arrow one tick (AbstractArrow.tick) against the targets;
// owner is the shooter's body, nil when gone. Water or rain extinguishes a
// burning arrow; last tick's stick is rechecked; a stuck arrow counts toward
// its despawn; a flying one eases its shaft toward the velocity, settles the
// owner exclusion, moves (clipped against blocks, then swept for bodies),
// and applies drag and gravity.
func (a *Arrow) Step(w World, owner *ArrowTarget, targets []ArrowTarget) []ArrowEvent {
	a.tickCount = min(a.tickCount+1, 8)
	events := a.step(w, owner, targets)
	if a.splash != nil {
		events = append(events, *a.splash)
		a.splash = nil
	}
	a.ticked = true
	return events
}

func (a *Arrow) step(w World, owner *ArrowTarget, targets []ArrowTarget) []ArrowEvent {
	if a.Dead {
		return nil
	}
	// vanilla: Entity.checkBelowWorld
	if a.Y < float64(w.MinY())-64 {
		a.Dead = true
		return nil
	}

	// The loyalty return runs before the arrow body, as ThrownTrident.tick
	// does: five stuck ticks (vanilla's >4) count as the blow dealt, and a
	// spent loyalty trident whose owner is present turns for the owner's eyes,
	// which is its whole tick. With the owner gone it lies until picked up or
	// despawned.
	if a.Trident {
		if a.Life > tridentStuckDealt && !a.dealtDamage {
			a.dealtDamage = true
		}
		if a.Loyalty > 0 && a.dealtDamage && owner != nil {
			return a.tridentReturnTick(w, owner)
		}
	}

	// vanilla: AbstractArrow.tick (clearFire when isInWaterOrRain), before
	// the in-ground branch so a stuck arrow is extinguished too.
	if a.FireTicks > 0 && (rules.IsWater(w.BlockAt(int32(math.Floor(a.X)), int32(math.Floor(a.Y)), int32(math.Floor(a.Z)))) ||
		rainLandsOn(w, a.X, a.Y, a.Z, ArrowHeight)) {
		a.FireTicks = 0
	}

	// Last tick's stick: the position is inside the collision shape of its
	// block. The return's noclip passes through instead.
	if !a.InGround && !a.NoPhysics && a.stuckIn(w) {
		a.VX, a.VY, a.VZ = 0, 0, 0
		a.InGround = true
		a.LastState = a.ownBlockState(w)
		// vanilla: onSyncedDataUpdated starts the shake only after the first tick.
		if a.ShakeTicks <= 0 && a.ticked {
			a.ShakeTicks = arrowShakeTicks
		}
	}

	if a.ShakeTicks > 0 {
		a.ShakeTicks--
	}

	if a.InGround {
		// The holding block may go (broken, or moved by a piston): the cell's
		// state differs and the point box is free, so the arrow is released and
		// the tick ends; the first fall step is next tick.
		if a.LastState != a.ownBlockState(w) && a.shouldFall(w) {
			a.startFalling()
			return nil
		}
		// tickDespawn: the life of a stuck arrow counts up to the 1200.
		a.Life++
		var faded []ArrowEvent
		if a.Life >= arrowPotionDecay && !a.Effect.IsPlain() {
			// vanilla: Arrow.tick, setPickupItemStack(new ItemStack(ARROW))
			a.PickupStack = protocol.ItemStack{}
			a.Effect = ArrowEffect{}
			faded = []ArrowEvent{{Kind: ArrowEffectFaded}}
		}
		douse := a.insideBlocks(w, a.X, a.Y, a.Z, nil)
		// A loyal trident its owner may pick up never despawns at 1200.
		// vanilla: ThrownTrident.tickDespawn
		if a.Life >= arrowLifeLimit && (!a.Trident || a.Loyalty <= 0 || a.Pickup != PickupAllowed) {
			a.Dead = true
			return append(faded, ArrowEvent{Kind: ArrowGone})
		}
		return append(faded, douse...)
	}

	// The move, the clip and the shaft's turn use the velocity from before
	// the water inertia; the hit, its damage and the next tick read the
	// slowed one. isInWater reads wasTouchingWater, which baseTick set at the
	// end of the last flight tick, not the box here.
	// vanilla: AbstractArrow.tick (movement, isInWater, applyInertia)
	mx, my, mz := a.VX, a.VY, a.VZ
	if a.inWater {
		inertia := a.waterInertia()
		a.VX *= inertia
		a.VY *= inertia
		a.VZ *= inertia
	}

	// The shaft eases a fifth of the way toward the velocity (lerpRotation).
	a.Yaw = lerpRotation(a.Yaw, arrowTargetYaw(mx, mz))
	a.Pitch = lerpRotation(a.Pitch, arrowTargetPitch(my, mx, mz))

	// The owner exclusion, settled once: when the grown flight box no longer
	// touches the shooter or its vehicle group, the arrow may hit them, and it
	// stays that way.
	// vanilla: Projectile.checkLeftOwner
	ownerVehicle := int32(0)
	if owner != nil {
		ownerVehicle = owner.RootVehicle
	}
	if !a.leftOwner && !a.leftOwnerChecked {
		if owner == nil {
			a.leftOwner = true
		} else {
			box := a.arrowBox()
			a.leftOwner = !ownerGroupTouching(&box, owner, ownerVehicle, targets)
		}
		a.leftOwnerChecked = true
	}

	// The move: clip against blocks first (a block hit ends the flight), then
	// sweep for bodies; the first met decides. The return's noclip meets
	// neither.
	if a.NoPhysics {
		a.SetPos(a.X+mx, a.Y+my, a.Z+mz)
		if !a.inWater {
			a.VX *= arrowAirDrag
			a.VY *= arrowAirDrag
			a.VZ *= arrowAirDrag
		}
		a.touchWater(w)
		a.leftOwnerChecked = false
		return nil
	}
	clip := clipProjectileFlight(w, a.X, a.Y, a.Z, mx, my, mz)
	blockState, blockHit := clip.state, clip.ok
	endX, endY, endZ := clip.end(a.X, a.Y, a.Z, mx, my, mz)

	// A piercing arrow carries on from each body it passes through toward the
	// same end point within this tick, one event per body.
	// vanilla: AbstractArrow.tick (the piercing loop), onHitEntity
	var hits []ArrowEvent
	sx, sy, sz := a.X, a.Y, a.Z
	for {
		hit, _, hitX, hitY, hitZ := sweepArrowTargets(w, a, targets, ownerVehicle, sx, sy, sz, endX, endY, endZ)
		if hit == nil {
			break
		}
		// The arrow stands at the hit point; the server's hurt decides whether
		// it is discarded or bounced (onHitEntity deflects a refused hit). A
		// piercing arrow's refused hit on an earlier body bounces at the final
		// position instead of that body's, which no client can tell apart within
		// one tick.
		a.SetPos(hitX, hitY, hitZ)
		hits = a.insideBlocks(w, sx, sy, sz, hits)
		if deflectsProjectile(hit, false) {
			// A deflection, or the same body's second contact, ends the
			// piercing with no hit; the arrow flies on from the hit point.
			// vanilla: AbstractArrow.stepMoveAndHit, hitTargetsOrDeflectSelf
			if hit.EntityID != a.lastDeflectedBy {
				reverseDeflection(&a.VX, &a.VY, &a.VZ, &a.Yaw, a.Rand, 1)
				a.lastDeflectedBy = hit.EntityID
				hits = append(hits, ArrowEvent{Kind: ArrowDeflected, Target: *hit, X: hitX, Y: hitY, Z: hitZ})
			}
			a.arrowTickTail(w)
			a.leftOwnerChecked = false
			return hits
		}
		ev := ArrowEvent{
			Kind:   ArrowHitTarget,
			Target: *hit,
			Damage: a.hitDamage(hit),
			X:      hitX, Y: hitY, Z: hitZ,
			VX: a.VX, VY: a.VY, VZ: a.VZ,
			IgniteTicks: a.igniteForTicks(),
			HitSound:    a.HitSound,
		}
		if a.PierceLevel <= 0 {
			return append(hits, ev)
		}
		if int32(len(a.pierced)) >= a.PierceLevel+1 {
			a.Dead = true
			// Hits already collected still land; only this body is discarded.
			// vanilla: AbstractArrow.tick
			return append(hits, ArrowEvent{Kind: ArrowGone})
		}
		a.pierced = append(a.pierced, hit.EntityID)
		hits = append(hits, ev)
		sx, sy, sz = hitX, hitY, hitZ
	}
	// The last leg runs to the end or the block's crossing, its block effects
	// before the block hit stops the arrow.
	// vanilla: AbstractArrow.stepMoveAndHit
	a.SetPos(endX, endY, endZ)
	hits = a.insideBlocks(w, sx, sy, sz, hits)
	if clip.border {
		// vanilla: Projectile.hitTargetOrDeflectSelf
		reverseDeflection(&a.VX, &a.VY, &a.VZ, &a.Yaw, a.Rand, 0.2)
		a.arrowTickTail(w)
		a.leftOwnerChecked = false
		return append(hits, ArrowEvent{Kind: ArrowDeflected, X: a.X, Y: a.Y, Z: a.Z})
	}
	if blockHit {
		// onHitBlock: draw the position back along the velocity's signs, zero the
		// velocity, clear critical, and wind the shake the clients animate when
		// the flag turns. The hit is measured at the crossing before the
		// draw-back (onProjectileHit).
		hit := BlockHit{
			X: clip.bx, Y: clip.by, Z: clip.bz,
			Face:  clip.face,
			HitX:  endX,
			HitY:  endY,
			HitZ:  endZ,
			State: blockState,
			Speed: math.Sqrt(a.VX*a.VX + a.VY*a.VY + a.VZ*a.VZ),
		}
		a.SetPos(endX-arrowStuckOffset*signum(a.VX), endY-arrowStuckOffset*signum(a.VY), endZ-arrowStuckOffset*signum(a.VZ))
		a.VX, a.VY, a.VZ = 0, 0, 0
		a.InGround = true
		a.LastState = blockState
		a.Crit = false
		a.ShakeTicks = arrowShakeTicks
		a.touchWater(w)
		// vanilla: AbstractArrow.onHitBlock
		hitSound := a.HitSound
		a.PierceLevel = 0
		a.HitSound = registry.SoundEntityArrowHit
		a.pierced = nil
		return append(hits, ArrowEvent{Kind: ArrowStuck, X: a.X, Y: a.Y, Z: a.Z, BlockHit: hit, HitSound: hitSound})
	}
	a.arrowTickTail(w)
	a.leftOwnerChecked = false
	return hits
}

// insideBlocks runs the block effects of the move from (fx, fy, fz) to the
// arrow's position over each cell its box swept, appending the events: a
// snowflake per powder snow cell while it flies sideways, a splash per
// bubble column surface it is pushed at, the ignition
// of lava and fire cells, and the extinguish of powder snow, layered
// cauldrons and water. Steps run in
// order; a step's melts and douses each need the arrow still burning, and
// run before that step's extinguish, so cells met in a later step find it
// already out.
// vanilla: Entity.applyEffectsFromBlocks, Entity.checkInsideBlocks,
// InsideBlockEffectApplier.StepBasedCollector
func (a *Arrow) insideBlocks(w World, fx, fy, fz float64, events []ArrowEvent) []ArrowEvent {
	if a.Dead || a.NoPhysics {
		return events // vanilla: Entity.isAffectedByBlocks
	}
	var before []ArrowEvent
	extinguish, fireIgnite, lavaIgnite, lastStep := false, false, false, -1
	freeze, clearFreeze := false, false
	flush := func() {
		// vanilla: InsideBlockEffectType order, FREEZE, CLEAR_FREEZE, FIRE_IGNITE, LAVA_IGNITE, EXTINGUISH
		applyFreeze(&a.TicksFrozen, freeze, clearFreeze)
		freeze, clearFreeze = false, false
		if fireIgnite {
			a.fireIgnite()
			fireIgnite = false
		}
		if lavaIgnite {
			a.FireTicks = max(a.FireTicks, fireTicksLava) // vanilla: Entity.lavaIgnite
			lavaIgnite = false
		}
		for _, ev := range before {
			if a.FireTicks > 0 {
				events = append(events, ev)
			}
		}
		before = before[:0]
		if extinguish {
			a.FireTicks = 0
			extinguish = false
		}
	}
	rel := AABB{-ArrowHalfSize, 0, -ArrowHalfSize, ArrowHalfSize, ArrowHeight, ArrowHalfSize}
	moves := [1]bodyMove{{fx: fx, fy: fy, fz: fz, tx: a.X, ty: a.Y, tz: a.Z}}
	var bubbles []bubbleSpot
	walkInside(w, moves[:], rel, func(x, y, z int32, state uint16, step int, from AABB, dx, dy, dz float64) bool {
		// vanilla: AbstractArrow.onInsideBubbleColumn, a stuck arrow is not pushed
		if !a.InGround {
			a.VY, _ = projectileBubblePush(w, x, y, z, state, from, dx, dy, dz, a.VY, false, &bubbles)
		}
		snow, cauldron := rules.IsPowderSnow(state), rules.IsLayeredCauldron(state)
		fire := rules.IsFire(state)
		fluid := fluidNone
		if !fire && !snow && !cauldron {
			fluid = sweptFluid(w, x, y, z, state, from, dx, dy, dz)
		}
		lava := fluid == fluidLava
		// Water puts the arrow out even while a lava or fire ignition is
		// still pending in this step, so an unlit arrow looks too.
		if !snow && !cauldron && !fire && fluid == fluidNone {
			return true
		}
		if step != lastStep {
			lastStep = step
			flush()
		}
		switch {
		case snow:
			// vanilla: PowderSnowBlock.entityInside
			freeze = true
			if a.VX != 0 || a.VZ != 0 {
				events = append(events, ArrowEvent{Kind: ArrowSnowflake, X: a.X, Y: float64(y + 1), Z: a.Z})
			}
			before = append(before, ArrowEvent{Kind: ArrowMeltedSnow, X: float64(x), Y: float64(y), Z: float64(z)})
		case cauldron:
			// vanilla: LayeredCauldronBlock.entityInside
			before = append(before, ArrowEvent{Kind: ArrowDousedCauldron, X: float64(x), Y: float64(y), Z: float64(z)})
		case fire:
			// vanilla: BaseFireBlock.entityInside
			fireIgnite, clearFreeze = true, true
		case lava:
			// vanilla: LavaFluid.entityInside
			lavaIgnite, clearFreeze = true, true
		}
		if !fire && !lava {
			extinguish = true
		}
		return true
	})
	flush()
	for _, p := range bubbles {
		events = append(events, ArrowEvent{Kind: ArrowBubbleSurface, X: float64(p.x), Y: float64(p.y), Z: float64(p.z)})
	}
	return events
}

// arrowTickTail ends AbstractArrow.tick after a move that did not stick: the
// burn counts down (baseTick; the arrow's own hurt is a no-op), then air drag
// unless the tick began in water (Step applied the water inertia instead),
// and gravity, then baseTick's water flag at the end position. Step runs
// the water-or-rain extinguish first. A deflection runs this in the hit's
// tick, in vanilla's order after stepMoveAndHit.
func (a *Arrow) arrowTickTail(w World) {
	if a.FireTicks > 0 {
		a.FireTicks--
	}
	if !a.inWater {
		a.VX *= arrowAirDrag
		a.VY *= arrowAirDrag
		a.VZ *= arrowAirDrag
	}
	if !a.NoGravity {
		a.VY -= arrowGravity
	}
	a.touchWater(w)
}

// touchWater sets inWater from the box at the arrow's position, as the
// flight tick's closing baseTick does, and records the splash of a first
// touch after the arrow's first tick.
// vanilla: Entity.updateFluidInteraction
func (a *Arrow) touchWater(w World) {
	was := a.inWater
	a.inWater, _, _ = BoxInWater(w, a.X, a.Y, a.Z, 2*ArrowHalfSize, ArrowHeight, a.Y)
	if a.inWater && !was && a.ticked {
		volume, pitch := splashSound(a.VX, a.VY, a.VZ, a.Rand)
		a.splash = &ArrowEvent{Kind: ArrowSplashed, X: a.X, Y: a.Y, Z: a.Z, Volume: volume, Pitch: pitch}
	}
}

// BlastBox is the arrow's box for ServerExplosion.getSeenPercent.
func (a *Arrow) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return a.X - ArrowHalfSize, a.Y, a.Z - ArrowHalfSize,
		a.X + ArrowHalfSize, a.Y + ArrowHeight, a.Z + ArrowHalfSize
}

// EyePosition is getEyePosition, at the type's eye height of 0.13, where an
// explosion's knockback is directed from.
func (a *Arrow) EyePosition() (x, y, z float64) {
	return a.X, a.Y + 0.13, a.Z
}

// Push is Entity.push for an explosion's knockback. A stuck arrow keeps the
// push in its velocity without moving until its block releases it.
func (a *Arrow) Push(vx, vy, vz float64) {
	a.VX += vx
	a.VY += vy
	a.VZ += vz
}

// Bounce is deflect(ProjectileDeflection.REVERSE) for a refused hit: the yaw
// turns by 170 + random.nextFloat()*20, the movement is scaled by -0.1 (a
// tenth of the speed, back the way it came), and the shaft turns to face
// it. The hit stays in place; the sweep ignores a start on the body's face.
// vanilla: AbstractArrow.onHitEntity, ProjectileDeflection.REVERSE
func (a *Arrow) Bounce(w World) {
	a.VX *= -0.1
	a.VY *= -0.1
	a.VZ *= -0.1
	a.Yaw += 170 + a.Rand.Float32()*20
	// A non-piercing hit returns from Step before the tick's drag and gravity;
	// piercing already ran the tail while collecting its hits.
	if a.PierceLevel <= 0 {
		a.arrowTickTail(w)
	}
}

// LeftOwner reports Projectile.leftOwner, which is saved so a loaded arrow
// is not treated as still beside its shooter. Once set it stays set.
func (a *Arrow) LeftOwner() bool {
	return a.leftOwner
}

// RestoreLeftOwner sets the shooter exclusion from a save.
func (a *Arrow) RestoreLeftOwner(left bool) { a.leftOwner = left }

// hitDamage is onHitEntity's damage: base damage (after
// EnchantmentHelper.modifyDamage on the weapon, in the hit's context:
// victim, direct attacker, no tool) times speed, clamped and ceiled, plus
// random.nextInt(damage / 2 + 2) when critical. The shooter's body is
// bound by BindLoot for loot conditions. Tridents use
// tridentHitDamage's fixed 8 instead.
func (a *Arrow) hitDamage(target *ArrowTarget) float64 {
	if a.Trident {
		return tridentHitDamage(a, target)
	}
	base := a.BaseDamage
	if len(a.Weapon.Enchantments) > 0 {
		ctx := &rules.EnchContext{
			DamageType:     rules.DamageTypeArrow,
			Direct:         false,
			Victim:         target.Type,
			DirectAttacker: protocol.EntityTypeArrow,
		}
		a.bindLoot(ctx, target)
		base = rules.EnchantedDamage(a.Weapon, ctx, base)
	}
	velocity := math.Sqrt(a.VX*a.VX + a.VY*a.VY + a.VZ*a.VZ)
	damage := math.Ceil(velocity * base)
	if damage < 0 {
		damage = 0
	}
	if damage > 2.147483647e9 {
		damage = 2.147483647e9
	}
	if a.Crit {
		damage += float64(a.Rand.Intn(int(damage)/2 + 2))
	}
	return damage
}

func (a *Arrow) bindLoot(ctx *rules.EnchContext, target *ArrowTarget) {
	if a.BindLoot != nil {
		a.BindLoot(ctx, target)
	}
}

// waterInertia is the velocity kept in water: 0.6 for an arrow, 0.99 for a
// trident.
func (a *Arrow) waterInertia() float64 {
	if a.Trident {
		return tridentWaterInertia
	}
	return arrowWaterInertia
}

// fireIgnite raises the burn counter the way a fire block does.
// vanilla: BaseFireBlock.fireIgnite (an arrow is not a player)
func (a *Arrow) fireIgnite() {
	if a.FireTicks < 0 {
		a.FireTicks++
	}
	if a.FireTicks >= 0 {
		a.FireTicks = max(a.FireTicks, fireTicksFire) // vanilla: Entity.igniteForSeconds
	}
}

// igniteForTicks is onHitEntity's igniteForSeconds(5), 100 ticks, only
// while the arrow burns. The caller raises the body's counter to at least
// this.
func (a *Arrow) igniteForTicks() int32 {
	if a.FireTicks <= 0 {
		return 0
	}
	return 100
}

// stuckIn reports whether the arrow's position is inside its block's
// collision shape, as the vanilla tick checks.
func (a *Arrow) stuckIn(w World) bool {
	x, y, z := int32(math.Floor(a.X)), int32(math.Floor(a.Y)), int32(math.Floor(a.Z))
	state := w.BlockAt(x, y, z)
	ox, oz := world.CollisionOffset(state, x, z)
	for _, s := range blockBoxes(state) {
		if a.X >= float64(x)+ox+s.MinX && a.X <= float64(x)+ox+s.MaxX &&
			a.Y >= float64(y)+s.MinY && a.Y <= float64(y)+s.MaxY &&
			a.Z >= float64(z)+oz+s.MinZ && a.Z <= float64(z)+oz+s.MaxZ {
			return true
		}
	}
	return false
}

// ownBlockState is the state at the arrow's cell, compared with lastState.
func (a *Arrow) ownBlockState(w World) uint16 {
	return w.BlockAt(int32(math.Floor(a.X)), int32(math.Floor(a.Y)), int32(math.Floor(a.Z)))
}

// shouldFall is AbstractArrow.shouldFall: no collision in the 0.12 cube
// about the position.
func (a *Arrow) shouldFall(w World) bool {
	return !overlapsSolid(w, noCtx, a.fallBox())
}

// fallBox is shouldFall's point box: the position grown by
// arrowFallInflate, not the arrow's own box.
func (a *Arrow) fallBox() AABB {
	return AABB{
		minX: a.X - arrowFallInflate, minY: a.Y - arrowFallInflate, minZ: a.Z - arrowFallInflate,
		maxX: a.X + arrowFallInflate, maxY: a.Y + arrowFallInflate, maxZ: a.Z + arrowFallInflate,
	}
}

// startFalling is AbstractArrow.startFalling: out of the ground, velocity
// scaled by a random up-to-a-fifth on each axis, and life reset so the
// despawn count restarts where it lands.
func (a *Arrow) startFalling() {
	a.InGround = false
	a.VX *= float64(a.Rand.Float32()) * 0.2
	a.VY *= float64(a.Rand.Float32()) * 0.2
	a.VZ *= float64(a.Rand.Float32()) * 0.2
	a.Life = 0
}

// flightClip is where a flight first met a block: the fraction travelled,
// the block's state and cell, and the entry face (a protocol.Face*). ok is
// false when no crossed cell held a box.
type flightClip struct {
	t          float64
	state      uint16
	bx, by, bz int32
	face       int32
	ok         bool
	border     bool
	// A border clamps each horizontal axis independently; its endpoint can
	// lie off the flight line, so t alone cannot represent it.
	x, y, z float64
}

// end returns the block crossing, the border's clamped point, or the miss endpoint.
func (c flightClip) end(x, y, z, dx, dy, dz float64) (float64, float64, float64) {
	if c.border {
		return c.x, c.y, c.z
	}
	if c.ok {
		dx, dy, dz = dx*c.t, dy*c.t, dz*c.t
	}
	return x + dx, y + dy, z + dz
}

type projectileBorder interface {
	borderClamper
	InBorderPoint(x, z float64) bool
}

// clipProjectileFlight includes the border only for a flight starting inside it.
// vanilla: CollisionGetter.clipIncludingBorder
func clipProjectileFlight(w World, x, y, z, dx, dy, dz float64) flightClip {
	clip := clipArrowFlight(w, x, y, z, dx, dy, dz)
	b, ok := w.(projectileBorder)
	if !ok || !b.InBorderPoint(x, z) {
		return clip
	}
	ex, ey, ez := clip.end(x, y, z, dx, dy, dz)
	if b.InBorderPoint(ex, ez) {
		return clip
	}
	face := projectileBorderFace(ex-x, ey-y, ez-z)
	ex, ez = b.ClampToBorder(ex, ez)
	bx, by, bz := int32(math.Floor(ex)), int32(math.Floor(ey)), int32(math.Floor(ez))
	return flightClip{ok: true, border: true, x: ex, y: ey, z: ez,
		bx: bx, by: by, bz: bz, face: face, state: w.BlockAt(bx, by, bz)}
}

// projectileBorderFace is Direction.getApproximateNearest, including float
// rounding and the enum's tie order: down, up, north, south, west, east.
func projectileBorderFace(dx, dy, dz float64) int32 {
	x, y, z := float32(dx), float32(dy), float32(dz)
	face, highest := int32(protocol.FaceNorth), float32(math.SmallestNonzeroFloat32)
	for i, dot := range [...]float32{-y, y, -z, z, -x, x} {
		if dot > highest {
			face, highest = int32(i), dot
		}
	}
	return face
}

// clipArrowFlight walks the cells the flight line crosses, nearest first,
// and reports where it first enters a collision shape (each cell's real
// boxes, as walking bodies read them).
func clipArrowFlight(w World, x, y, z, dx, dy, dz float64) flightClip {
	return clipBlockShapes(w, x, y, z, dx, dy, dz, nil)
}

// clipBlockShapes clips a segment against collision boxes, ignoring blocks
// marked in skip when a visual sight ray sees through them.
func clipBlockShapes(w World, x, y, z, dx, dy, dz float64, skip *[registry.BlockCount]bool) flightClip {
	var clip flightClip
	if dx == 0 && dy == 0 && dz == 0 {
		return clip
	}
	const epsilon = 1e-7
	cx, cy, cz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	tx, ty, tz := int32(math.Floor(x+dx)), int32(math.Floor(y+dy)), int32(math.Floor(z+dz))
	stepX, stepY, stepZ := signOf(dx), signOf(dy), signOf(dz)
	var tMaxX, tMaxY, tMaxZ, tDeltaX, tDeltaY, tDeltaZ float64
	if stepX != 0 {
		boundary := float64(cx)
		if stepX > 0 {
			boundary = float64(cx + 1)
		}
		tMaxX = (boundary - x) / dx
		tDeltaX = math.Abs(1 / dx)
	} else {
		tMaxX = math.Inf(1)
		tDeltaX = math.Inf(1)
	}
	if stepY != 0 {
		boundary := float64(cy)
		if stepY > 0 {
			boundary = float64(cy + 1)
		}
		tMaxY = (boundary - y) / dy
		tDeltaY = math.Abs(1 / dy)
	} else {
		tMaxY = math.Inf(1)
		tDeltaY = math.Inf(1)
	}
	if stepZ != 0 {
		boundary := float64(cz)
		if stepZ > 0 {
			boundary = float64(cz + 1)
		}
		tMaxZ = (boundary - z) / dz
		tDeltaZ = math.Abs(1 / dz)
	} else {
		tMaxZ = math.Inf(1)
		tDeltaZ = math.Inf(1)
	}

	best := math.Inf(1)
	found := false
	askCell := func(cx, cy, cz int32) {
		cell := w.BlockAt(cx, cy, cz)
		if skip != nil && skip[registry.BlockOf(cell)] {
			return
		}
		ox, oz := world.CollisionOffset(cell, cx, cz)
		for _, s := range blockBoxes(cell) {
			if t, face, ok := world.SegmentAABB(x, y, z, dx, dy, dz,
				float64(cx)+ox+s.MinX, float64(cy)+s.MinY, float64(cz)+oz+s.MinZ,
				float64(cx)+ox+s.MaxX, float64(cy)+s.MaxY, float64(cz)+oz+s.MaxZ); ok && t >= -epsilon && t < best {
				best = t
				found = true
				clip.state = cell
				clip.bx, clip.by, clip.bz = cx, cy, cz
				clip.face = face
			}
		}
	}
	askCell(cx, cy, cz)
	for !found && (cx != tx || cy != ty || cz != tz) {
		if tMaxX <= tMaxY && tMaxX <= tMaxZ {
			if tMaxX > 1+epsilon {
				break
			}
			cx += stepX
			tMaxX += tDeltaX
		} else if tMaxY <= tMaxZ {
			if tMaxY > 1+epsilon {
				break
			}
			cy += stepY
			tMaxY += tDeltaY
		} else {
			if tMaxZ > 1+epsilon {
				break
			}
			cz += stepZ
			tMaxZ += tDeltaZ
		}
		askCell(cx, cy, cz)
	}
	if !found {
		return clip
	}
	clip.t = math.Max(0, math.Min(1, best))
	clip.ok = true
	return clip
}

// sweepArrowTargets returns the body whose box the flight line enters
// first, the fraction, and the entry point (an EntityHitResult's location).
// Near misses use the age-grown margin if the shell is visible.
// A box already containing the arrow is passed through. The shooter and its
// vehicle group are skipped until left, and a spent trident meets nobody
// (findHitEntity).
func sweepArrowTargets(w World, a *Arrow, targets []ArrowTarget, ownerVehicle int32, fromX, fromY, fromZ, endX, endY, endZ float64) (*ArrowTarget, float64, float64, float64, float64) {
	if a.Trident && a.dealtDamage {
		return nil, math.Inf(1), 0, 0, 0
	}
	dx, dy, dz := endX-fromX, endY-fromY, endZ-fromZ
	// vanilla: ProjectileUtil.computeMargin
	margin := float64(max(0, min(float32(0.3), float32(a.tickCount-2)/20)))
	minX, minY, minZ := math.Min(fromX, endX)-margin, math.Min(fromY, endY)-margin, math.Min(fromZ, endZ)-margin
	maxX, maxY, maxZ := math.Max(fromX, endX)+margin, math.Max(fromY, endY)+margin, math.Max(fromZ, endZ)+margin
	var best *ArrowTarget
	bestT := math.Inf(1)
	var hx, hy, hz float64
	for i := range targets {
		t := &targets[i]
		if t.MaxX < minX || t.MinX > maxX || t.MaxY < minY || t.MinY > maxY || t.MaxZ < minZ || t.MinZ > maxZ {
			continue
		}
		if t.Unhittable {
			continue // vanilla: canBeHitByProjectile, the spectator's no
		}
		if t.Type == protocol.EntityTypePlayer && (a.SparePlayers || slices.Contains(a.SpareIDs, t.EntityID)) {
			continue // vanilla: AbstractArrow.canHitEntity, the pvp and team gates
		}
		if ownerExcluded(a.OwnerID, ownerVehicle, a.leftOwner, t) {
			continue // the shooter's own body and vehicle group, until left
		}
		if slices.Contains(a.pierced, t.EntityID) || slices.Contains(a.Passed, t.EntityID) {
			continue // vanilla: AbstractArrow.canHitEntity, piercingIgnoreEntityIds
		}
		tx, okT := arrowTargetEntry(w, t, margin, fromX, fromY, fromZ, dx, dy, dz)
		if !okT || tx >= bestT {
			continue
		}
		best, bestT = t, tx
		hx, hy, hz = fromX+dx*tx, fromY+dy*tx, fromZ+dz*tx
	}
	return best, bestT, hx, hy, hz
}

// arrowTargetEntry prefers the exact box crossing. A near miss may hit the
// age-grown margin only when the ray from that point toward the body's centre
// reaches the shell before a block; arrows keep the outside hit location.
// vanilla: ProjectileUtil.getManyEntityHitResult (projectSurfaceHitLocation=false)
func arrowTargetEntry(w World, target *ArrowTarget, margin, x, y, z, dx, dy, dz float64) (float64, bool) {
	entry := func(x, y, z, dx, dy, dz, grow float64) (float64, bool) {
		t, _, ok := world.SegmentAABB(x, y, z, dx, dy, dz,
			target.MinX-grow, target.MinY-grow, target.MinZ-grow,
			target.MaxX+grow, target.MaxY+grow, target.MaxZ+grow)
		return t, ok && t > 0 && t < 1
	}
	if t, ok := entry(x, y, z, dx, dy, dz, 0); ok {
		return t, true
	}
	if margin <= 0 {
		return 0, false
	}
	t, ok := entry(x, y, z, dx, dy, dz, margin)
	if !ok {
		return 0, false
	}
	hx, hy, hz := x+dx*t, y+dy*t, z+dz*t
	cx, cy, cz := (target.MinX+target.MaxX)/2-hx, (target.MinY+target.MaxY)/2-hy, (target.MinZ+target.MaxZ)/2-hz
	if clip := clipArrowFlight(w, hx, hy, hz, cx, cy, cz); clip.ok {
		cx, cy, cz = cx*clip.t, cy*clip.t, cz*clip.t
	}
	_, ok = entry(hx, hy, hz, cx, cy, cz, 0)
	return t, ok
}

// ownerExcluded reports whether a projectile that has not left its shooter
// must skip the target: the shooter or anything sharing its root vehicle.
// vanilla: Projectile.canHitEntity, Entity.isPassengerOfSameVehicle
func ownerExcluded(ownerID, ownerVehicle int32, leftOwner bool, target *ArrowTarget) bool {
	if leftOwner {
		return false
	}
	if ownerID != 0 && target.EntityID == ownerID {
		return true
	}
	return ownerVehicle != 0 && target.RootVehicle == ownerVehicle
}

// ownerGroupTouching reports whether box touches the shooter or its vehicle
// group (Projectile.isOutsideOwnerCollisionRange's noneMatch).
func ownerGroupTouching(box, owner *ArrowTarget, ownerVehicle int32, targets []ArrowTarget) bool {
	if boxesTouch(box, owner) {
		return true
	}
	if ownerVehicle == 0 {
		return false
	}
	for i := range targets {
		if targets[i].RootVehicle == ownerVehicle && boxesTouch(box, &targets[i]) {
			return true
		}
	}
	return false
}

// signOf is the velocity's sign along an axis, 0 for none.
func signOf(d float64) int32 {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// arrowTargetYaw and arrowTargetPitch are the velocity's atan2 angles, which
// the shaft eases toward.
func arrowTargetYaw(vx, vz float64) float32 {
	return float32(math.Atan2(vx, vz) * 180 / math.Pi)
}

func arrowTargetPitch(vy, vx, vz float64) float32 {
	return float32(math.Atan2(vy, math.Hypot(vx, vz)) * 180 / math.Pi)
}

// lerpRotation is Entity.lerpRotation: a fifth of the way toward the target
// the short way round.
func lerpRotation(current, target float32) float32 {
	delta := float64(target - current)
	delta = math.Mod(delta+540, 360) - 180
	return current + float32(delta*0.2)
}

// CanPickUp is playerTouch's check before tryPickup: in the ground, the
// shake settled, the pickup rule allows this player, and not yet discarded
// (a despawn on the arrow's last tick must not hand it to a touch that
// same tick).
func (a *Arrow) CanPickUp(creative bool) bool {
	if a.Dead || !a.InGround || a.ShakeTicks > 0 {
		return false
	}
	switch a.Pickup {
	case PickupAllowed:
		return true
	case PickupCreativeOnly:
		return creative
	}
	return false
}

// DragonPartTarget exposes a dragon part to the projectile sweep.
// vanilla: Level.getEntities, EnderDragonPart.isPickable
func DragonPartTarget(m *Mob, typ int32, part int) ArrowTarget {
	b := m.DragonPartBox(part)
	return ArrowTarget{EntityID: m.ID + 1 + int32(part), Type: typ, MinX: b.MinX, MinY: b.MinY, MinZ: b.MinZ, MaxX: b.MaxX, MaxY: b.MaxY, MaxZ: b.MaxZ, EyeY: b.MinY + (b.MaxY-b.MinY)*0.85}
}

// MobProjectileTargets appends the parent (for owner lookup) and pickable parts.
func MobProjectileTargets(dst []ArrowTarget, m *Mob, typ int32, root int32) []ArrowTarget {
	target := MobTarget(m.ID, typ, m)
	target.RootVehicle = root
	dst = append(dst, target)
	if m.Type == TypeEnderDragon {
		for part := range 8 {
			target = DragonPartTarget(m, typ, part)
			target.RootVehicle = root
			dst = append(dst, target)
		}
	}
	return dst
}
