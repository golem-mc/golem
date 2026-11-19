package terrain

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// Dimension picks a noise settings preset. vanilla: NoiseGeneratorSettings
type Dimension int

// The presets the terrain engine supports.
const (
	Overworld Dimension = iota
	OverworldLarge
	OverworldAmplified
	Nether
	End
)

// AquiferConfig is the overworld's aquifer functions. vanilla: Aquifer.Config
type AquiferConfig struct {
	Barrier, FluidLevelFloodedness, FluidLevelSpread, Lava, Exclusion, SurfaceLevel Sampler
}

// Router is the set of density functions a chunk generator reads.
// vanilla: NoiseRouter
type Router struct {
	Temperature, Vegetation, Continents, Erosion, Depth, Ridges Sampler
	ChunkSurfaceLevel, FinalDensity                             Sampler
	// Aquifer is nil when the dimension has no aquifers.
	Aquifer *AquiferConfig
	// Veins is nil when the dimension has no ore veins.
	Veins *VeinConfig
}

// VeinConfig is the density functions of the overworld's ore veins.
// vanilla: OreVeinRule
type VeinConfig struct {
	Copper, Iron  Vein
	Richness, Gap Sampler
}

// Vein is one ore's density, which is -1 outside minY..maxY (inclusive).
type Vein struct {
	Density    Sampler
	MinY, MaxY int32
}

// RandomState is a world's seeded noise and the router built from it.
// vanilla: RandomState
type RandomState struct {
	Seed   int64
	Router Router
	// MinY and Height are the noise settings' vertical extent.
	MinY, Height int32
	SeaLevel     int32
	defaultFluid Fluid
	legacy       bool
	random       levelgen.PositionalRandomFactory
	noises       map[string]*NoiseStack
	noiseMu      sync.Mutex
	graph        *graph
	// Materials builds the surface and ore veins of a chunk.
	Materials *MaterialSystem
}

// NewRandomState seeds the dimension's noises and builds its router.
// vanilla: RandomState.create
func NewRandomState(dim Dimension, seed int64) *RandomState {
	rs := &RandomState{Seed: seed, noises: map[string]*NoiseStack{}, graph: newGraph()}
	switch dim {
	case Nether:
		rs.legacy = true
		rs.MinY, rs.Height, rs.SeaLevel = 0, 128, 32
		rs.defaultFluid = FluidLava
	case End:
		rs.legacy = true
		rs.MinY, rs.Height, rs.SeaLevel = 0, 128, 0
		rs.defaultFluid = FluidAir
	default:
		rs.MinY, rs.Height, rs.SeaLevel = -64, 384, 63
		rs.defaultFluid = FluidWater
	}
	if rs.legacy {
		rs.random = levelgen.NewLegacyRandom(seed).ForkPositional()
	} else {
		rs.random = levelgen.NewXoroshiroRandom(seed).ForkPositional()
	}
	switch dim {
	case Nether:
		rs.Router = rs.netherRouter()
	case End:
		rs.Router = rs.endRouter()
	default:
		rs.Router = rs.overworldRouter(dim)
	}
	switch dim {
	case Nether:
		rs.Materials = newMaterialSystem(rs, netherRule, Netherrack)
	case End:
		rs.Materials = newMaterialSystem(rs, endRule, EndStone)
	default:
		rs.Materials = newMaterialSystem(rs, overworldRule, Stone)
	}
	return rs
}

// Noise returns the registered noise of that name, drawn once.
// vanilla: RandomState.getOrCreateNoise
func (rs *RandomState) Noise(name string) *NoiseStack {
	// Rules resolve additional noises while separate chunks compile their contexts.
	rs.noiseMu.Lock()
	defer rs.noiseMu.Unlock()
	if n, ok := rs.noises[name]; ok {
		return n
	}
	var n *NoiseStack
	switch name {
	case "minecraft:nether/temperature":
		n = NoiseByName(name).CreateForLegacyNetherBiome(levelgen.NewLegacyRandom(rs.Seed))
	case "minecraft:nether/vegetation":
		n = NoiseByName(name).CreateForLegacyNetherBiome(levelgen.NewLegacyRandom(rs.Seed + 1))
	default:
		n = NoiseByName(name).Create(rs.random.FromHashOf(name))
	}
	rs.noises[name] = n
	return n
}

// RandomFactory returns the positional factory of that name.
// vanilla: RandomState.getOrCreateRandomFactory
func (rs *RandomState) RandomFactory(name string) levelgen.PositionalRandomFactory {
	return rs.random.FromHashOf(name).ForkPositional()
}

// terrainRandom is the stream of the base terrain noise. vanilla: RandomState.createRandom
func (rs *RandomState) terrainRandom() levelgen.RandomSource {
	if rs.legacy {
		return levelgen.NewLegacyRandom(rs.Seed)
	}
	return rs.random.FromHashOf("minecraft:terrain")
}

// slide fades the density towards a target at the top and bottom.
// vanilla: NoiseRouterData.slide
func slide(caves Sampler, minY, height, topStartY, topEndY int32, topTarget float32, bottomStartY, bottomEndY int32, bottomTarget float32) Sampler {
	topFactor := YClampedGradient(minY+height-topStartY, minY+height-topEndY, 1.0, 0.0)
	noiseValue := Lerp(topFactor, constant(topTarget), caves)
	bottomFactor := YClampedGradient(minY+bottomStartY, minY+bottomEndY, 0.0, 1.0)
	return Lerp(bottomFactor, constant(bottomTarget), noiseValue)
}

// postProcess is vanilla: NoiseRouterData.postProcess.
func postProcess(slide Sampler, cellSizeXZ, cellSizeY int32) Sampler {
	return Squeeze(Interpolated(Mul(BlendDensity(slide), constant(0.64)), cellSizeXZ, cellSizeY))
}

// slideNetherLike is vanilla: NoiseRouterData.slideNetherLike.
func slideNetherLike(base3d Sampler, minY, height int32) Sampler {
	return slide(base3d, minY, height, 24, 0, 0.9375, -8, 24, 2.5)
}

// slideEndLike is vanilla: NoiseRouterData.slideEndLike.
func slideEndLike(caves Sampler, minY, height int32) Sampler {
	return slide(caves, minY, height, 72, -184, -23.4375, 4, 32, -0.234375)
}

// netherRouter is vanilla: NoiseRouterData.nether.
func (rs *RandomState) netherRouter() Router {
	base3d := NewBlendedNoise(rs.terrainRandom(), 0.25, 0.375, 80.0, 60.0, 8.0)
	zero := constant(0)
	temperature := ShiftedNoise2D(zero, zero, 0.25, rs.Noise("minecraft:nether/temperature"))
	vegetation := ShiftedNoise2D(zero, zero, 0.25, rs.Noise("minecraft:nether/vegetation"))
	slideN := slideNetherLike(base3d, 0, 128)
	full := Add(postProcess(slideN, 4, 8), BeardifierSampler())
	return Router{Temperature: temperature, Vegetation: vegetation, Continents: zero, Erosion: zero, Depth: zero,
		Ridges: zero, ChunkSurfaceLevel: zero, FinalDensity: full}
}

// endRouter is vanilla: NoiseRouterData.end.
func (rs *RandomState) endRouter() Router {
	base3d := NewBlendedNoise(rs.terrainRandom(), 0.25, 0.25, 80.0, 160.0, 4.0)
	// vanilla: NoiseRouterData.createEndIslands
	mainIsland := Slice(AxisY, 0, Mul(Sub(Clamp(Sub(constant(100.0), DistanceToPoint(0, 0, 0)), -100.0, 80.0), constant(8.0)), constant(0.0078125)))
	outer := EndOuterIslands(levelgen.NewLegacyRandom(rs.Seed))
	islands := Slice(AxisY, 0, rs.graph.Cache(Max(mainIsland, outer)))
	slopedCheeseEnd := Add(islands, base3d)
	full := Add(postProcess(slideEndLike(slopedCheeseEnd, 0, 128), 8, 4), BeardifierSampler())
	zero := constant(0)
	return Router{Temperature: zero, Vegetation: zero, Continents: zero, Erosion: islands, Depth: zero,
		Ridges: zero, ChunkSurfaceLevel: zero, FinalDensity: full}
}
