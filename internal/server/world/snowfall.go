package world

import (
	"strconv"

	"github.com/golem-mc/golem/registry"
)

// The sky's work, from the random tick pass: one face in 48 of a ticking
// chunk's dice hands its column to tickPrecipitation, which freezes water, lays
// snow while it rains (up to max_snow_accumulation_height) and fills open
// cauldrons.
// vanilla: ServerLevel.tickPrecipitation

const (
	// precipitationDie is the dice faces the sky's work waits behind: one in 48
	// of the random_tick_speed dice per chunk picks a column.
	precipitationDie = 48
	// cauldronRainChance and cauldronSnowChance are a cauldron's fill rolls:
	// rain 1 in 20, snow 1 in 10.
	// vanilla: CauldronBlock.shouldHandlePrecipitation
	cauldronRainChance float32 = 0.05
	cauldronSnowChance float32 = 0.1
)

// cauldronFullLevel is LayeredCauldronBlock's ceiling.
const cauldronFullLevel = 3

// iceState, snowByLayers, snowLayerCounts and the cauldron tables are built
// once so the pass parses no state properties.
var (
	iceState                      = registry.MustState("minecraft:ice", nil)
	snowByLayers, snowLayerCounts = snowLayerStates()
	waterCauldronByLevel          = cauldronLevels("minecraft:water_cauldron")
	powderSnowCauldronByLevel     = cauldronLevels("minecraft:powder_snow_cauldron")
)

// snowLayerStates builds the snow-layer tables over the layers property.
func snowLayerStates() ([9]uint16, map[uint16]int) {
	var states [9]uint16
	counts := make(map[uint16]int, 8)
	for layers := 1; layers <= 8; layers++ {
		id, ok := registry.WithInt(registry.BlockSnow.DefaultState(), registry.PropLayers, layers)
		if !ok {
			continue
		}
		states[layers] = id
		counts[id] = layers
	}
	return states, counts
}

// cauldronLevels builds the levels of a filling cauldron, index zero unused.
func cauldronLevels(name string) [4]uint16 {
	var levels [4]uint16
	for level := 1; level <= cauldronFullLevel; level++ {
		id, ok := registry.FindState(name, map[string]string{"level": strconv.Itoa(level)})
		if !ok {
			continue
		}
		levels[level] = id
	}
	return levels
}

// tickPrecipitation: the MOTION_BLOCKING top block freezes (rain or not) where
// the biome is too cold to rain and block light is under 10; while the rain
// level is past the gameplay threshold, a cold sky lays one snow layer per
// visit at the first free cell, up to maxHeight; and the block below the top
// takes the precipitation (the cauldrons).
// vanilla: ServerLevel.tickPrecipitation
func (p *Physics) tickPrecipitation(x, z int32, maxHeight int32, changes *[]BlockChange) {
	if !p.w.ChunkLoadedAt(x, z) {
		return
	}
	top := p.w.motionBlockingHeight(x, z) + 1
	climate := p.w.Biome(x, top, z).Climate()
	below := top - 1
	if p.freezesAt(x, below, z, climate) {
		if ice, ok := p.veto(HookForm, x, below, z, iceState, [3]int32{}); ok {
			p.setShaped(x, below, z, ice, changes)
		}
	}
	if !p.w.IsRaining() {
		return
	}
	if maxHeight > 0 && p.snowsAt(x, top, z, climate) {
		state := p.w.BlockAt(x, top, z)
		if layers := snowLayerCounts[state]; layers == 0 || int32(layers) < min(maxHeight, 8) {
			// Air lays the first layer; a pile grows one toward the rule's
			// height.
			if next, ok := p.veto(HookForm, x, top, z, snowByLayers[layers+1], [3]int32{}); ok {
				p.raiseSnow(x, top, z, state, next, changes)
			}
		}
	}
	if precip := p.w.precipitationFrom(climate, x, below, z); precip != precipNone {
		p.blockTakesPrecipitation(x, below, z, precip, changes)
	}
}

// freezesAt: too cold to rain, within the build height, block light under 10,
// and a water source not surrounded by water on all four sides (the open side
// lets the cold in).
// vanilla: Biome.shouldFreeze
func (p *Physics) freezesAt(x, y, z int32, climate registry.BiomeClimate) bool {
	if p.w.warmEnoughToRain(x, y, z, climate) {
		return false
	}
	if !contains(y) || int(p.w.BlockLightAt(x, y, z)) >= 10 {
		return false
	}
	if p.w.BlockAt(x, y, z) != waterByLevel[0] {
		return false
	}
	for _, d := range horizontal4 {
		if fluidHolding(p.w.BlockAt(x+d[0], y, z+d[1])).kind != fluidWater {
			return true
		}
	}
	return false
}

// snowsAt: a snowing biome, within the build height, block light under 10, and
// air or a snow layer that a layer survives on.
// vanilla: Biome.shouldSnow
func (p *Physics) snowsAt(x, y, z int32, climate registry.BiomeClimate) bool {
	if p.w.precipitationFrom(climate, x, y, z) != precipSnow {
		return false
	}
	if !contains(y) || int(p.w.BlockLightAt(x, y, z)) >= 10 {
		return false
	}
	if !p.airAt(x, y, z) && !p.blockIs(x, y, z, registry.BlockSnow) {
		return false
	}
	return p.w.Survives(x, y, z, snowByLayers[1])
}

// raiseSnow lays or grows the layer, carrying the collision rise for
// Block.pushEntitiesUp.
// vanilla: ServerLevel.tickPrecipitation
func (p *Physics) raiseSnow(x, y, z int32, old, next uint16, changes *[]BlockChange) {
	n := len(*changes)
	p.setShaped(x, y, z, next, changes)
	if len(*changes) > n {
		(*changes)[n].PushUp = CollisionTop(next) - CollisionTop(old)
	}
}

// blockTakesPrecipitation is Block.handlePrecipitation for the block below the
// top: an empty cauldron takes a level of water in rain or powder snow in snow,
// a filling one rises a level per successful roll until full, and a lava
// cauldron never fills. The roll comes before the state is read, so a full
// cauldron spends the draw too.
// vanilla: CauldronBlock.handlePrecipitation,
// LayeredCauldronBlock.handlePrecipitation
func (p *Physics) blockTakesPrecipitation(x, y, z int32, precip precipitation, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if int(state) >= registry.StateCount {
		return
	}
	b := registry.BlockOf(state)
	switch b {
	case registry.BlockCauldron, registry.BlockWaterCauldron, registry.BlockPowderSnowCauldron:
	default:
		return
	}
	if precip != precipRain && precip != precipSnow {
		return
	}
	chance := cauldronSnowChance
	if precip == precipRain {
		chance = cauldronRainChance
	}
	if p.rng.Float32() >= chance {
		return
	}
	written := len(*changes)
	level, _ := registry.StateInt(state, registry.PropLevel)
	switch b {
	case registry.BlockCauldron:
		if precip == precipRain {
			p.setCauldron(x, y, z, waterCauldronByLevel[1], changes)
		} else {
			p.setCauldron(x, y, z, powderSnowCauldronByLevel[1], changes)
		}
	case registry.BlockWaterCauldron:
		if precip == precipRain && level < cauldronFullLevel {
			p.setCauldron(x, y, z, waterCauldronByLevel[level+1], changes)
		}
	case registry.BlockPowderSnowCauldron:
		if precip == precipSnow && level < cauldronFullLevel {
			p.setCauldron(x, y, z, powderSnowCauldronByLevel[level+1], changes)
		}
	}
	if len(*changes) > written {
		p.randomEffects = append(p.randomEffects, Effect{GameEvent: gameEventBlockChange, X: x, Y: y, Z: z})
	}
}

// cauldronEmptyState is the plain cauldron a layered one empties to.
var cauldronEmptyState = registry.MustState("minecraft:cauldron", nil)

// CauldronDoused lowers a layered cauldron a level for a burning body inside
// (powder snow melting to water, the last level leaving an empty cauldron) and
// returns the change for the server to broadcast. A cell no longer a layered
// cauldron is left alone.
// vanilla: LayeredCauldronBlock.handleEntityOnFireInside, lowerFillLevel
func (p *Physics) CauldronDoused(x, y, z int32) []BlockChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if b != registry.BlockWaterCauldron && b != registry.BlockPowderSnowCauldron {
		return nil
	}
	level, ok := registry.StateInt(state, registry.PropLevel)
	if !ok || level <= 0 {
		return nil
	}
	next := cauldronEmptyState
	if level > 1 {
		next = waterCauldronByLevel[level-1]
	}
	var changes []BlockChange
	p.setCauldron(x, y, z, next, &changes)
	return changes
}

// snowMeltLight is the block light above which a snow layer melts.
// vanilla: SnowLayerBlock.randomTick
const snowMeltLight = 11

// snowLayerRandomTick removes a layer whose cell has block light above
// snowMeltLight and posts block_destroy. Nothing drops: the loot needs a
// tool-holding entity.
// vanilla: SnowLayerBlock.randomTick
func (p *Physics) snowLayerRandomTick(x, y, z int32, changes *[]BlockChange) {
	if int(p.w.BlockLightAt(x, y, z)) > snowMeltLight {
		air, ok := p.veto(HookFade, x, y, z, airState, [3]int32{})
		if !ok {
			return
		}
		p.setShaped(x, y, z, air, changes)
		p.randomEffects = append(p.randomEffects, Effect{GameEvent: gameEventBlockDestroy, X: x, Y: y, Z: z})
	}
}
