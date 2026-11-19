package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// livingFireContact applies the block bites shared by living bodies.
// vanilla: BaseFireBlock.fireIgnite, CampfireBlock.entityInside
func livingFireContact(w World, box AABB, fire *int, ignite func(int), hurt func(float64, registry.DamageType)) {
	ignited := false
	box.insideCells(func(x, y, z int32) {
		state := w.BlockAt(x, y, z)
		if amount, lit := rules.CampfireDamage(state); lit {
			hurt(amount, rules.DamageTypeCampfire)
			return
		}
		if !rules.IsFire(state) {
			return
		}
		damage := fireBlockDamage
		if rules.IsSoulFire(state) {
			damage = soulFireDamage
		}
		if !ignited {
			ignited = true
			if fireContactIgnites(fire) {
				ignite(fireTicksFire)
			}
		}
		hurt(float64(damage), rules.DamageTypeInFire)
	})
}

// livingLavaContact includes cauldrons, which the fluid tracker does not read.
// vanilla: LavaFluid.entityInside, LavaCauldronBlock.entityInside
func livingLavaContact(w World, box AABB, inLava bool, ignite func(int), hurt func(float64, registry.DamageType)) {
	if !inLava {
		box.insideCells(func(x, y, z int32) { inLava = inLava || rules.IsLavaCauldron(w.BlockAt(x, y, z)) })
	}
	if inLava {
		ignite(fireTicksLava)
		hurt(float64(lavaDamage), rules.DamageTypeLava)
	}
}

// livingFireTick returns whether water or rain extinguished a burning body.
// vanilla: Entity.baseTick
func livingFireTick(ticks *int, wet, immune, inLava bool, hurt func(float64, registry.DamageType)) bool {
	if *ticks == 0 {
		return false
	}
	if wet {
		burning := *ticks > 0 && !immune
		*ticks = 0
		return burning
	}
	if immune {
		*ticks = 0
		return false
	}
	if *ticks > 0 {
		if *ticks%20 == 0 && !inLava {
			hurt(1, rules.DamageTypeOnFire)
		}
		*ticks--
	}
	return false
}

// livingAirTick returns whether the air floor calls for a drowning bite.
// vanilla: LivingEntity.baseTick, decreaseAirSupply, increaseAirSupply
func livingAirTick(air *int, maxAir int, submerged, drain, refill bool, spend func() bool, increase func(int) int) bool {
	if submerged && drain {
		if spend() {
			*air--
		}
		if *air <= rules.AirDrownFloor {
			*air = 0
			return true
		}
	} else if *air < maxAir && (!submerged || refill) {
		*air = increase(*air)
	}
	return false
}

// increaseLivingAirSupply refills ordinary living bodies by four per tick.
// vanilla: LivingEntity.increaseAirSupply
func increaseLivingAirSupply(air int) int {
	return min(air+airRefillPerTick, rules.MaxAirSupply)
}

// fireContactIgnites advances the cooldown once per contact step.
// vanilla: BaseFireBlock.fireIgnite
func fireContactIgnites(fire *int) bool {
	if *fire < 0 {
		*fire++
	}
	return *fire >= 0
}
