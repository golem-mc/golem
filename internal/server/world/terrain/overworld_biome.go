package terrain

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// OverworldBiome is a biome in the overworld's climate parameter list.
type OverworldBiome uint8

// Name is the biome's registry path.
func (b OverworldBiome) Name() string { return overworldBiomeNames[b] }

var overworldIndex = sync.OnceValue(func() *levelgen.ClimateIndex {
	// vanilla: Climate.RTree.CHILDREN_PER_NODE
	return levelgen.NewClimateIndex(overworldBiomePoints[:], 19)
})

// OverworldBiomeSource samples the float router and searches its climate index,
// or answers one biome everywhere when fixed.
// vanilla: MultiNoiseBiomeSource, FixedBiomeSource
type OverworldBiomeSource struct {
	router Router
	fixed  *OverworldBiome
}

// NewOverworldBiomeSource uses the climate fields of an overworld random state.
func NewOverworldBiomeSource(state *RandomState) *OverworldBiomeSource {
	return &OverworldBiomeSource{router: state.Router}
}

// NewFixedOverworldBiomeSource answers b at every quart.
// vanilla: FixedBiomeSource
func NewFixedOverworldBiomeSource(b OverworldBiome) *OverworldBiomeSource {
	return &OverworldBiomeSource{fixed: &b}
}

// NewFixedOverworldBiomeSourceFor answers b at every quart and keeps the
// state's climate fields for FindSpawnPosition, which reads the climate
// whatever the biome source.
// vanilla: FixedBiomeSource, MinecraftServer.setInitialSpawn
func NewFixedOverworldBiomeSourceFor(state *RandomState, b OverworldBiome) *OverworldBiomeSource {
	return &OverworldBiomeSource{router: state.Router, fixed: &b}
}

// OverworldBiomeSampler owns one climate context and the search's previous result.
// vanilla: Climate.Sampler, Climate.RTree.lastResult
type OverworldBiomeSampler struct {
	fixed   *OverworldBiome
	router  Router
	context *Context
	search  *levelgen.ClimateSearch
}

// NewSampler starts a search with no previous result; it belongs to one goroutine.
func (s *OverworldBiomeSource) NewSampler() *OverworldBiomeSampler {
	if s.fixed != nil {
		return &OverworldBiomeSampler{fixed: s.fixed}
	}
	return &OverworldBiomeSampler{router: s.router, context: NewUncachedContext(), search: overworldIndex().NewSearch()}
}

// quantizeClimate is vanilla: Climate.quantizeCoord.
func quantizeClimate(value float32) int64 { return int64(float32(value * 10000)) }

// At is the biome at a quart position. vanilla: Climate.Sampler.sample
func (s *OverworldBiomeSampler) At(qx, qy, qz int32) OverworldBiome {
	if s.fixed != nil {
		return *s.fixed
	}
	x, y, z := qx<<2, qy<<2, qz<<2
	r, c := s.router, s.context
	target := [6]int64{
		quantizeClimate(r.Temperature.Sample(c, x, y, z)),
		quantizeClimate(r.Vegetation.Sample(c, x, y, z)),
		quantizeClimate(r.Continents.Sample(c, x, y, z)),
		quantizeClimate(r.Erosion.Sample(c, x, y, z)),
		quantizeClimate(r.Depth.Sample(c, x, y, z)),
		quantizeClimate(r.Ridges.Sample(c, x, y, z)),
	}
	return OverworldBiome(s.search.Find(target))
}

// FindSpawnPosition searches the source's climate with depth forced to zero.
// vanilla: Climate.Sampler.findSpawnPosition
func (s *OverworldBiomeSource) FindSpawnPosition() (x, z int32) {
	r, c := s.router, NewUncachedContext()
	return levelgen.FindSpawnPosition(func(qx, qz int32) [6]int64 {
		x, z := qx<<2, qz<<2
		return [6]int64{
			quantizeClimate(r.Temperature.Sample(c, x, 0, z)),
			quantizeClimate(r.Vegetation.Sample(c, x, 0, z)),
			quantizeClimate(r.Continents.Sample(c, x, 0, z)),
			quantizeClimate(r.Erosion.Sample(c, x, 0, z)),
			0,
			quantizeClimate(r.Ridges.Sample(c, x, 0, z)),
		}
	}, levelgen.OverworldSpawnTargets())
}

// At samples a quart with no previous search result.
func (s *OverworldBiomeSource) At(qx, qy, qz int32) OverworldBiome {
	return s.NewSampler().At(qx, qy, qz)
}

// FillChunk visits quarts section by section, x then y then z within a section.
// The search's previous result must follow this order to resolve climate ties.
// vanilla: MultiNoiseBiomeSource.createResolverForChunk, ChunkAccess.fillBiomesFromNoise
func (s *OverworldBiomeSource) FillChunk(cx, cz int32, set func(x, y, z int32, b OverworldBiome)) {
	if s.fixed != nil {
		for x := range int32(4) {
			for y := range int32(96) {
				for z := range int32(4) {
					set(x, y, z, *s.fixed)
				}
			}
		}
		return
	}
	v := NewVolume(4, 96, 4, cx*16, -64, cz*16)
	v.StepX, v.StepY, v.StepZ = 4, 4, 4
	ctx := NewContext(nil)
	defer ctx.recycle()
	var values [6][]float32
	for i, sampler := range [...]Sampler{s.router.Temperature, s.router.Vegetation, s.router.Continents, s.router.Erosion, s.router.Depth, s.router.Ridges} {
		values[i] = ctx.acquire(v.Size())
		sampler.SampleVolume(ctx, values[i], v)
	}
	search := overworldIndex().NewSearch()
	for section := range int32(24) {
		for x := range int32(4) {
			for y := section * 4; y < section*4+4; y++ {
				for z := range int32(4) {
					i := v.Index(x, y, z)
					var target [6]int64
					for axis := range target {
						target[axis] = quantizeClimate(values[axis][i])
					}
					set(x, y, z, OverworldBiome(search.Find(target)))
				}
			}
		}
	}
	for _, buf := range values {
		ctx.release(buf)
	}
}
