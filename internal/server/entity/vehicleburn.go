package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vehicleBurnPass applies per-cell hurts in effect-step order, stopping when
// a hurt removes the vehicle. Ignition is deduplicated per step; hurts are not.
// Powder snow adds one frozen tick per step (once however many cells it meets),
// lava clears the count. No non-living entity type is in freeze_immune_entity_types.
// A layered cauldron met while on fire is appended to doused, and powder snow
// to melted, as the step's extinguish runs.
// vanilla: InsideBlockEffectApplier.StepBasedCollector, Entity.applyEffectsFromBlocks
func vehicleBurnPass(w World, move bodyMove, rel AABB, fire, frozen *int, dead *bool, hurt func(float64, registry.DamageType, bool) bool, visit insideVisitor, doused, melted *[][3]int32) (burns, fizzles int) {
	if *dead {
		return
	}
	wasOnFire := *fire > 0
	previousFire := *fire
	var fireBuf [16]float32
	fires := fireBuf[:0]
	lava := 0
	extinguish := false
	var cauldrons, snows [][3]int32
	lastStep := -1
	flush := func() {
		if *dead {
			return
		}
		if len(snows) > 0 {
			*frozen = min(rules.FullyFrozenTicks, *frozen+1)
		}
		if len(fires) > 0 {
			// vanilla: BaseFireBlock.fireIgnite (a vehicle is not a player)
			if fireContactIgnites(fire) {
				*fire = max(*fire, fireTicksFire)
			}
			for _, damage := range fires {
				if *dead {
					return
				}
				hurt(float64(damage), rules.DamageTypeInFire, false)
			}
		}
		if *dead {
			return
		}
		if lava > 0 {
			*frozen = 0 // vanilla: InsideBlockEffectType.CLEAR_FREEZE
			*fire = max(*fire, fireTicksLava)
			for range lava {
				if *dead {
					return
				}
				if hurt(float64(lavaDamage), rules.DamageTypeLava, false) {
					burns++
				}
			}
		}
		if !*dead && extinguish {
			// vanilla: LayeredCauldronBlock.entityInside and
			// PowderSnowBlock.entityInside act only for a body still on fire.
			if *fire > 0 {
				*doused = append(*doused, cauldrons...)
				*melted = append(*melted, snows...)
			}
			*fire = 0
		}
		cauldrons, snows = cauldrons[:0], snows[:0]
		fires = fires[:0]
		lava = 0
		extinguish = false
	}
	walkInside(w, []bodyMove{move}, rel, func(x, y, z int32, state uint16, step int, from AABB, dx, dy, dz float64) bool {
		if step != lastStep {
			flush()
			lastStep = step
		}
		if *dead {
			return false
		}
		if visit != nil {
			visit(x, y, z, state, step, from, dx, dy, dz)
		}
		switch {
		case rules.IsFire(state):
			damage := fireBlockDamage
			if rules.IsSoulFire(state) {
				damage = soulFireDamage
			}
			fires = append(fires, damage)
		case rules.IsPowderSnow(state):
			snows = append(snows, [3]int32{x, y, z})
			extinguish = true
		case rules.IsLayeredCauldron(state):
			cauldrons = append(cauldrons, [3]int32{x, y, z})
			extinguish = true
		case sweptLavaCauldron(x, y, z, state, from, dx, dy, dz):
			lava++
		default:
			switch sweptFluid(w, x, y, z, state, from, dx, dy, dz) {
			case fluidLava:
				lava++
			case fluidWater:
				extinguish = true
			}
		}
		return true
	})
	flush()
	if *fire > 0 && rainLandsOn(w, move.tx, move.ty, move.tz, rel.maxY-rel.minY) {
		*fire = 0
	}
	if wasOnFire && *fire <= 0 {
		fizzles++
	}
	// vanilla: Entity.getFireImmuneTicks (vehicles return zero)
	if *fire <= 0 && *fire <= previousFire {
		*fire = 0
	}
	return
}
