package world

import "github.com/golem-mc/golem/registry"

// placeSnowAndFreeze is the freeze_top_layer feature: over the chunk's 16 x 16
// columns, the water cell under the motion-blocking top freezes and the cell
// on top takes a snow layer where the biome is cold enough, turning a snowy
// block underneath snowy. Both read the biome at the top cell. Block light
// reads 0 here: features run before the chunk is lit.
// vanilla: SnowAndFreezeFeature.place
func (r *decorRegion) placeSnowAndFreeze(climateAt func(r *decorRegion, x, y, z int32) registry.BiomeClimate, origin [3]int32) {
	for dx := range int32(sectionSize) {
		for dz := range int32(sectionSize) {
			x, z := origin[0]+dx, origin[2]+dz
			y := r.heightAt(func(s uint16) bool { return motionBlockingByState[s] }, x, z)
			below := y - 1
			climate := climateAt(r, x, y, z)
			if heightAdjustedTemperature(SeaLevel, x, below, z, climate) < rainTemperatureFloor &&
				r.inBuildHeight(below) && registry.BlockOf(r.get(x, below, z)) == registry.BlockWater {
				r.set(x, below, z, iceState)
			}
			if !r.shouldSnow(climate, x, y, z) {
				continue
			}
			r.set(x, y, z, snowByLayers[1])
			if s, ok := registry.WithBool(r.get(x, below, z), registry.PropSnowy, true); ok {
				r.set(x, below, z, s)
			}
		}
	}
}

func (r *decorRegion) inBuildHeight(y int32) bool { return y >= r.minY && y < r.minY+r.height }

// shouldSnow is Biome.shouldSnow with block light 0: snow falls in the biome
// at this height and a layer can stand on the cell below.
// vanilla: Biome.shouldSnow
func (r *decorRegion) shouldSnow(climate registry.BiomeClimate, x, y, z int32) bool {
	if !climate.Precipitation || heightAdjustedTemperature(SeaLevel, x, y, z, climate) >= rainTemperatureFloor || !r.inBuildHeight(y) {
		return false
	}
	if s := r.get(x, y, z); !r.src.isAir(s) && registry.BlockOf(s) != registry.BlockSnow {
		return false
	}
	return snowCanSurviveOn(r.get(x, y-1, z))
}

// snowCanSurviveOn is SnowLayerBlock.canSurvive over the block below.
// vanilla: SnowLayerBlock.canSurvive
func snowCanSurviveOn(below uint16) bool {
	b := registry.BlockOf(below)
	switch {
	case b.InTag(registry.TagIDCannotSupportSnowLayer):
		return false
	case b.InTag(registry.TagIDSupportOverrideSnowLayer):
		return true
	}
	return registry.CollisionTopFull(below) || below == snowByLayers[8]
}
