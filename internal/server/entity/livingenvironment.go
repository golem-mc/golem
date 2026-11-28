package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// livingFireContact applies the block bites shared by living bodies.
// vanilla: BaseFireBlock.fireIgnite, CampfireBlock.entityInside
func livingFireContact(w World, box AABB, fire *int, ignite func(ticks int, x, y, z int32), hurt func(float64, registry.DamageType)) {
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
				ignite(fireTicksFire, x, y, z)
			}
		}
		hurt(float64(damage), rules.DamageTypeInFire)
	})
}

// livingLavaContact includes cauldrons, which the fluid tracker does not read.
// vanilla: LavaFluid.entityInside, LavaCauldronBlock.entityInside
func livingLavaContact(w World, box AABB, inLava bool, ignite func(ticks int, x, y, z int32), hurt func(float64, registry.DamageType)) {
	var lx, ly, lz int32
	found := false
	box.insideCells(func(x, y, z int32) {
		state := w.BlockAt(x, y, z)
		if rules.IsLava(state) || rules.IsLavaCauldron(state) {
			inLava = true
			if !found {
				lx, ly, lz, found = x, y, z, true
			}
		}
	})
	if inLava {
		if !found {
			lx = int32(math.Floor((box.minX + box.maxX) / 2))
			ly = int32(math.Floor(box.minY))
			lz = int32(math.Floor((box.minZ + box.maxZ) / 2))
		}
		ignite(fireTicksLava, lx, ly, lz)
		hurt(float64(lavaDamage), rules.DamageTypeLava)
	}
}

// livingFireTick burns a body and counts its fire down; water is not tested
// here, it douses later in the tick (see douseFire).
// vanilla: Entity.baseTick
func livingFireTick(ticks *int, immune, inLava bool, hurt func(float64, registry.DamageType)) {
	if *ticks == 0 {
		return
	}
	if immune {
		*ticks = 0
		return
	}
	if *ticks > 0 {
		if *ticks%20 == 0 && !inLava {
			hurt(1, rules.DamageTypeOnFire)
		}
		*ticks--
	}
}

// douseFire puts out a burning body touching water or rain and reports
// whether it was burning.
// vanilla: WaterFluid.entityInside, Entity.applyEffectsFromBlocks
func douseFire(ticks *int, wet bool) bool {
	if !wet || *ticks <= 0 {
		return false
	}
	*ticks = 0
	return true
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
