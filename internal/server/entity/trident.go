package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The thrown trident, from the ThrownTrident and TridentItem bytecode:
// an AbstractArrow whose pickup is the thrown stack, so the arrow's flight
// and sticking carry it. This file holds ThrownTrident's differences: the
// fixed 8 damage and bounce, the loyalty return, and water inertia 0.99
// instead of 0.6.
// vanilla: ThrownTrident, TridentItem

// ThrownTrident and TridentItem numbers: hit damage (BASE_DAMAGE); water
// inertia (WATER_INERTIA); the stuck ticks before the blow counts as dealt
// (tick's inGroundTime > 4); and the return's 0.015 per level climb toward
// the owner's eyes, 0.05 of velocity toward them, 0.95 damping of the old
// velocity, the return sound's volume (TridentReturnVolume), and the 127
// clamp on loyalty (Mth.clamp of trident_return_acceleration, which is the
// level).
const (
	TridentBaseDamage   = 8.0
	tridentWaterInertia = 0.99
	tridentStuckDealt   = 4
	tridentEyeClimb     = 0.015
	tridentAccel        = 0.05
	tridentDampen       = 0.95
	// TridentReturnVolume is the one-time return sound's volume of 10, heard
	// far across the field.
	TridentReturnVolume = 10.0
	tridentLoyaltyCap   = 127
)

var channelingEnchantment, _ = registry.EnchantmentID("minecraft:channeling")

// NewTrident places a thrown trident moving along its velocity. The thrown
// stack (wear and enchantments) is both the pickup and the hit's
// enchantment source; its loyalty is clamped to the synced byte.
// vanilla: ThrownTrident constructor, getLoyaltyFromItem
func NewTrident(id int32, x, y, z, vx, vy, vz float64, pickup ArrowPickup, ownerID int32, stack protocol.ItemStack, seed int64, contexts ...*rules.EnchContext) *Arrow {
	// baseDamage stays the arrow's 2.0: ThrownTrident never sets it, so a
	// vanilla file saves that and the fixed 8 is a constant.
	a := NewArrow(id, x, y, z, vx, vy, vz, false, ArrowBaseDamage, pickup, ownerID, seed)
	a.Trident = true
	a.HitSound = registry.SoundItemTridentHitGround
	a.Channeling = max(stack.Enchantments[channelingEnchantment], 0)
	a.Weapon = stack
	ctx := rules.EnchContext{Victim: protocol.EntityTypeTrident, Attacker: -1, DirectAttacker: -1}
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = *contexts[0]
	}
	ctx.Rand = a.Rand
	a.Loyalty = min(rules.TridentReturnAcceleration(stack, &ctx), tridentLoyaltyCap)
	return a
}

// TridentDeflect is the end of ThrownTrident.onHitEntity: the blow is
// marked dealt (the sweep skips all bodies from now) and the reverse
// deflection is scaled to almost nothing, the yaw turned by its roll. The
// caller runs it once the hurt is settled, whatever it came to; vanilla
// deflects a spent trident regardless. arrowTickTail then applies drag and
// gravity, vanilla's order after stepMoveAndHit.
// vanilla: ProjectileDeflection.REVERSE, ThrownTrident.onHitEntity
func (a *Arrow) TridentDeflect(w World) {
	a.dealtDamage = true
	a.VX *= -0.5
	a.VY *= -0.5
	a.VZ *= -0.5
	a.VX *= 0.02
	a.VY *= 0.2
	a.VZ *= 0.02
	a.Yaw += 170 + a.Rand.Float32()*20
	a.arrowTickTail(w)
}

// tridentReturnTick is a returning trident's whole tick, replacing the
// AbstractArrow tick (ThrownTrident.tick): noclip on, climb toward the
// owner's eye level and accelerate toward the eyes by the loyalty share,
// passing through anything. A non-player owner cannot take it, so a trident
// within its reach is finished rather than left orbiting. The first return
// tick plays the sound.
// vanilla: ThrownTrident.tick
func (a *Arrow) tridentReturnTick(w World, owner *ArrowTarget) []ArrowEvent {
	if a.ShakeTicks > 0 {
		a.ShakeTicks--
	}
	first := !a.NoPhysics

	aimX := (owner.MinX + owner.MaxX) / 2
	aimY := owner.EyeY
	aimZ := (owner.MinZ + owner.MaxZ) / 2
	if !first && owner.Type != protocol.EntityTypePlayer &&
		math.Sqrt((a.X-aimX)*(a.X-aimX)+(a.Y-aimY)*(a.Y-aimY)+(a.Z-aimZ)*(a.Z-aimZ)) < owner.MaxX-owner.MinX+1 {
		a.Dead = true
		return []ArrowEvent{{Kind: ArrowGone}}
	}
	a.NoPhysics = true

	vecX, vecY, vecZ := aimX-a.X, aimY-a.Y, aimZ-a.Z
	length := math.Sqrt(vecX*vecX + vecY*vecY + vecZ*vecZ)
	loyalty := float64(a.Loyalty)
	a.Y += vecY * tridentEyeClimb * loyalty
	a.VX = a.VX * tridentDampen
	a.VY = a.VY * tridentDampen
	a.VZ = a.VZ * tridentDampen
	// At the owner's eyes the aim is zero and normalizes to nothing, so only
	// the old velocity remains.
	if length >= 1e-4 {
		a.VX += vecX / length * tridentAccel * loyalty
		a.VY += vecY / length * tridentAccel * loyalty
		a.VZ += vecZ / length * tridentAccel * loyalty
	}

	// A returning trident flies shaft-first: rotation eases toward the
	// negated movement (the tick's !physicsEnabled branch).
	a.Yaw = lerpRotation(a.Yaw, arrowTargetYaw(-a.VX, -a.VZ))
	a.Pitch = lerpRotation(a.Pitch, arrowTargetPitch(a.VY, a.VX, a.VZ))

	a.X += a.VX
	a.Y += a.VY
	a.Z += a.VZ
	// Air drag still applies on the return; gravity does not.
	a.VX *= arrowAirDrag
	a.VY *= arrowAirDrag
	a.VZ *= arrowAirDrag
	a.touchWater(w)

	if !first {
		return nil
	}
	return []ArrowEvent{{Kind: ArrowTridentReturn, X: a.X, Y: a.Y, Z: a.Z}}
}

// DealtDamage reports whether the trident has dealt its blow (a hit, or
// five stuck ticks); saved, and read by the sweep and the return. Arrows
// report false.
func (a *Arrow) DealtDamage() bool {
	return a.dealtDamage
}

// RestoreDealtDamage sets the dealt-damage mark from a save.
func (a *Arrow) RestoreDealtDamage(dealt bool) {
	a.dealtDamage = dealt
}

// tridentHitDamage is a trident's onHitEntity damage: its fixed 8 plus the
// thrown stack's damage enchantments (impaling's 2.5 a level against
// aquatic mobs), in a context with the trident damage type and the trident
// as direct attacker. No speed scaling and no critical roll.
// vanilla: ThrownTrident.onHitEntity, EnchantmentHelper.modifyDamage
func tridentHitDamage(a *Arrow, target *ArrowTarget) float64 {
	damage := TridentBaseDamage
	if len(a.Weapon.Enchantments) > 0 {
		ctx := &rules.EnchContext{
			DamageType:     rules.DamageTypeTrident,
			Direct:         false,
			Victim:         target.Type,
			DirectAttacker: protocol.EntityTypeTrident,
		}
		a.bindLoot(ctx, target)
		damage = rules.EnchantedDamage(a.Weapon, ctx, damage)
	}
	return damage
}
