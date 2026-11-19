package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// worldGenSculkTuning is the spreader as world generation runs it, with the
// region's random and no sounds.
// vanilla: SculkSpreader.createWorldGenSpreader
var worldGenSculkTuning = sculkTuning{
	growthCost: 50, noGrowthRadius: 1, decayRate: 5, additionalDecay: 10,
	replaceable: registry.TagIDSculkReplaceableWorldGen, worldGen: true,
}

// sculkPatchConfig is a sculk_patch feature's configuration.
// vanilla: SculkPatchFeature
type sculkPatchConfig struct {
	chargeCount, amountPerCharge, spreadAttempts, growthRounds, spreadRounds int
}

var sculkAncientCityFeature = sync.OnceValue(func() featureFn {
	return nestedPlaced(parseJSON(levelgen.PlacedFeatureJSON("sculk_patch_ancient_city")))
})

// jigsawFeatures are the placed features a feature pool element may name;
// each has no placement modifiers.
var jigsawFeatures = map[string]func(r *decorRegion, x, y, z int32) bool{
	"minecraft:sculk_patch_ancient_city": func(r *decorRegion, x, y, z int32) bool {
		return sculkAncientCityFeature()(r, [3]int32{x, y, z})
	},
}

// genSculkWorld is the sculkWorld over a decoration region.
type genSculkWorld struct{ r *decorRegion }

func (w genSculkWorld) get(x, y, z int32) uint16 { return w.r.get(x, y, z) }

func (w genSculkWorld) set(x, y, z int32, state uint16, _ bool) bool {
	w.r.set(x, y, z, state)
	return true
}

func (genSculkWorld) sound(int32, int32, int32, registry.Sound) {}
func (genSculkWorld) placedSculk(int32, int32, int32, uint16)   {}

// genSculkRandom shuffles as Util.shuffle does.
type genSculkRandom struct{ levelgen.RandomSource }

func (g genSculkRandom) Perm(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	shuffle(order, g)
	return order
}

// place grows the patch at the origin, false when nothing is there to spread
// from.
// vanilla: SculkPatchFeature.place
func (c sculkPatchConfig) place(r *decorRegion, x, y, z int32) bool {
	if !genSculkCanSpreadFrom(r, x, y, z) {
		return false
	}
	rng := r.rng
	s := &sculkSpreader{w: genSculkWorld{r}, rng: genSculkRandom{rng}, t: &worldGenSculkTuning, origin: blockKey{x, y, z}}
	var cursors []chargeCursor
	for round := range c.spreadRounds + c.growthRounds {
		for range c.chargeCount {
			for charge := c.amountPerCharge; charge > 0; charge -= sculkMaxCharge {
				if len(cursors) < sculkMaxCursors {
					cursors = append(cursors, chargeCursor{x: x, y: y, z: z, charge: min(charge, sculkMaxCharge), decayDelay: 1})
				}
			}
		}
		for range c.spreadAttempts {
			kept := cursors[:0]
			for _, cur := range cursors {
				s.update(&cur, round < c.spreadRounds)
				if cur.charge > 0 {
					kept = append(kept, cur)
				}
			}
			cursors = kept
		}
		cursors = cursors[:0]
	}
	return true
}

// genSculkCanSpreadFrom wants sculk or vein, or air or a water source with a
// full block around it.
func genSculkCanSpreadFrom(r *decorRegion, x, y, z int32) bool {
	state := r.get(x, y, z)
	if sculkKindOf(state) != sculkOther {
		return true
	}
	if !r.src.isAir(state) && (!isWater(state) || !isWaterSource(state)) {
		return false
	}
	for _, o := range protocol.FaceOffset {
		if registry.CollisionShapeFull(r.get(x+o[0], y+o[1], z+o[2])) {
			return true
		}
	}
	return false
}
