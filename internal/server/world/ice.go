package world

import (
	"math/rand"
	"strconv"

	"github.com/golem-mc/golem/registry"
)

// Frosted ice from Frost Walker: it ages on its own scheduled tick and melts
// when a neighbour change leaves it thin.
// vanilla: FrostedIceBlock.tick
// vanilla: FrostedIceBlock.neighborChanged

// FrostedIceBlock's numbers: a 1-in-3 age-or-melt roll per tick, faster aging
// with fewer than four frosted neighbours, melting when a changed neighbour
// leaves fewer than two, and light past 11 minus age and dampening to melt.
const (
	frostedIceMaxAge           = 3
	frostedIceNeighboursToAge  = 4
	frostedIceNeighboursToMelt = 2
	frostedIceMeltLight        = 11
)

// frostedIcePlaceDelay is onPlace's wait before the first tick:
// Mth.nextInt(random, 60, 120).
func frostedIcePlaceDelay(rng *rand.Rand) int64 {
	return int64(60 + rng.Intn(61))
}

// frostedIceMeltDelay is the tick's wait between visits, for itself and the
// neighbours a melt schedules: Mth.nextInt(random, 20, 40).
func frostedIceMeltDelay(rng *rand.Rand) int64 {
	return int64(20 + rng.Intn(21))
}

// frostedIceAges and frostedIceStateByAge map states to ages and back, built
// once so a tick parses no properties.
var frostedIceAges, frostedIceStateByAge = func() (map[uint16]int, [frostedIceMaxAge + 1]uint16) {
	ages := make(map[uint16]int, frostedIceMaxAge+1)
	var states [frostedIceMaxAge + 1]uint16
	for age := 0; age <= frostedIceMaxAge; age++ {
		id, ok := registry.FindState("minecraft:frosted_ice", map[string]string{"age": strconv.Itoa(age)})
		if !ok {
			continue
		}
		states[age] = id
		ages[id] = age
	}
	return ages, states
}()

// frostedIceAgeOf is a frosted-ice state's age, zero for other blocks.
func frostedIceAgeOf(state uint16) int { return frostedIceAges[state] }

// frostedIceFewerNeighboursThan reports fewer than limit frosted-ice neighbours
// of any age (vanilla's fewerNeigboursThan).
func (p *Physics) frostedIceFewerNeighboursThan(x, y, z int32, limit int) bool {
	count := 0
	for _, d := range dirDelta {
		if registry.BlockOf(p.w.BlockAt(x+d[0], y+d[1], z+d[2])) == registry.BlockFrostedIce {
			count++
			if count >= limit {
				return false
			}
		}
	}
	return true
}

// meltIce turns ice to water, posting block_destroy into effects, or to air
// with no event where the dimension evaporates water.
// vanilla: IceBlock.melt
func (p *Physics) meltIce(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	if p.w.DimensionName() == NetherName {
		if air, ok := p.veto(HookFade, x, y, z, airState, [3]int32{}); ok {
			p.setShaped(x, y, z, air, changes)
		}
		return
	}
	water, ok := p.veto(HookFade, x, y, z, waterByLevel[0], [3]int32{})
	if !ok {
		return
	}
	p.setShaped(x, y, z, water, changes)
	*effects = append(*effects, Effect{GameEvent: gameEventBlockDestroy, X: x, Y: y, Z: z})
}

// slightlyMeltFrostedIce ages the ice a step with a flag-2 write (shape pass,
// no neighbour woken), or melts it at the ceiling; it reports which.
// vanilla: FrostedIceBlock.slightlyMelt
func (p *Physics) slightlyMeltFrostedIce(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	age := frostedIceAgeOf(state)
	if age < frostedIceMaxAge {
		p.setClientShaped(x, y, z, frostedIceStateByAge[age+1], changes)
		return false
	}
	p.meltIce(x, y, z, state, changes, &p.effects)
	return true
}

// frostedIceTick offers a melt when the dice favour it or fewer than four
// neighbours remain, and the light is over 11 minus age and dampening (block
// light alone at the endpoint, otherwise the brighter of block and sky). A full
// melt ages the neighbours one step, scheduling those that only aged; a block
// that stayed books its next visit.
// vanilla: FrostedIceBlock.tick
func (p *Physics) frostedIceTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age := frostedIceAgeOf(state)
	if p.rng.Intn(3) == 0 || p.frostedIceFewerNeighboursThan(x, y, z, frostedIceNeighboursToAge) {
		brightness := int(p.w.MaxLocalRawLightAt(x, y, z))
		if p.w.DimensionName() == EndName {
			brightness = int(p.w.BlockLightAt(x, y, z))
		}
		if brightness > frostedIceMeltLight-age-registry.LightDampening(state) &&
			p.slightlyMeltFrostedIce(x, y, z, state, changes) {
			for _, d := range dirDelta {
				nx, ny, nz := x+d[0], y+d[1], z+d[2]
				neighbour := p.w.BlockAt(nx, ny, nz)
				if registry.BlockOf(neighbour) != registry.BlockFrostedIce {
					continue
				}
				if !p.slightlyMeltFrostedIce(nx, ny, nz, neighbour, changes) {
					p.schedule(nx, ny, nz, p.now+frostedIceMeltDelay(p.rng))
				}
			}
			return
		}
	}
	p.schedule(x, y, z, p.now+frostedIceMeltDelay(p.rng))
}

// iceRandomTick melts ice at once when block light exceeds 11
// minus its dampening.
// vanilla: IceBlock.randomTick
func (p *Physics) iceRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if int(p.w.BlockLightAt(x, y, z)) > frostedIceMeltLight-registry.LightDampening(state) {
		p.meltIce(x, y, z, state, changes, &p.randomEffects)
	}
}
