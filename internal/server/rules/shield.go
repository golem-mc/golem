package rules

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// A raised item per its blocks_attacks: when it blocks, how much, the wear
// and the disabling. Packets, item wear and raised state are the server's.

// ShieldBlockDelayTicks is the shield's own block delay in ticks, its
// block_delay_seconds 0.25 × 20, for a caller that waits one out.
const ShieldBlockDelayTicks int32 = 5

// BlockDelayTicks is how long the item must have been raised before it
// blocks. vanilla: BlocksAttacks.blockDelayTicks
func BlockDelayTicks(b *registry.BlocksAttacks) int32 {
	return int32(math.Floor(float64(b.BlockDelaySeconds)*20 + 0.5))
}

// BlockEngages reports whether an item raised for raisedTicks takes a hurt of
// damageType into itself at all, before the angle is looked at: the delay is
// over and the type is not one bypassed_by names.
// vanilla: LivingEntity.getItemBlockingWith, LivingEntity.applyItemBlocking
func BlockEngages(b *registry.BlocksAttacks, raisedTicks int32, damageType registry.DamageType) bool {
	return raisedTicks >= BlockDelayTicks(b) &&
		(b.BypassedBy == nil || !b.BypassedBy.Contains(damageType))
}

// BlockAngle is the angle in radians between the look of a holder at (vx, vz)
// facing yaw (degrees) and the way to the source at (sx, sz), in the
// horizontal plane. A source at the holder's own spot is square to the look.
// vanilla: LivingEntity.applyItemBlocking
func BlockAngle(yaw, vx, vz, sx, sz float64) float64 {
	ux, uz := KnockbackFacing(yaw)
	dx, dz := sx-vx, sz-vz
	if n := math.Sqrt(dx*dx + dz*dz); n >= 1e-5 {
		dx, dz = dx/n, dz/n
	} else {
		dx, dz = 0, 0
	}
	return math.Acos(ux*dx + uz*dz)
}

// BlockNoSourceAngle is the angle of a hurt with no source position, which
// only a reduction of 180 degrees or more takes.
const BlockNoSourceAngle = math.Pi

// ResolveBlockedDamage is how much of a hurt the item blocks: the sum of the
// reductions that match the damage type and the angle, clamped to the damage.
// vanilla: BlocksAttacks.resolveBlockedDamage, DamageReduction.resolve
func ResolveBlockedDamage(b *registry.BlocksAttacks, damageType registry.DamageType, angle, damage float64) float64 {
	var blocked float64
	for _, r := range b.Reductions {
		if angle > float64(float32(math.Pi)/180*r.Angle) {
			continue
		}
		if r.Type != nil && !r.Type.Contains(damageType) {
			continue
		}
		blocked += math.Min(math.Max(float64(r.Base)+float64(r.Factor)*damage, 0), damage)
	}
	return math.Min(math.Max(blocked, 0), damage)
}

// BlockItemDamage is the wear blocking the given amount costs the item:
// nothing under item_damage's threshold, else the floor of base plus factor
// times the amount. vanilla: BlocksAttacks.ItemDamageFunction.apply
func BlockItemDamage(b *registry.BlocksAttacks, blocked float64) int32 {
	f := b.ItemDamage
	if blocked < float64(f.Threshold) {
		return 0
	}
	return int32(math.Floor(float64(f.Base) + float64(f.Factor)*blocked))
}

// BlockDisableTicks is the cooldown a blow disabling for baseSeconds puts on
// the item, 0 when the blow disables nothing.
// vanilla: BlocksAttacks.disableBlockingForTicks
func BlockDisableTicks(b *registry.BlocksAttacks, baseSeconds float32) int32 {
	seconds := baseSeconds * b.DisableCooldownScale
	if seconds > 0 {
		return int32(math.Floor(float64(seconds*20) + 0.5))
	}
	return 0
}
