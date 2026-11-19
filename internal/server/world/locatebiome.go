package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// BiomeSampler is a generator that can name the biome of any quart position
// without a world around it, which /locate biome searches by.
type BiomeSampler interface {
	// PossibleBiomes is every biome the generator can produce, as wire names.
	// vanilla: BiomeSource.possibleBiomes
	PossibleBiomes() []string
	// NewBiomeProbe returns a reader of the biome at a quart position (block
	// coordinate >> 2), which may keep per-search state and is not safe for
	// concurrent use.
	// vanilla: BiomeSource.getNoiseBiome
	NewBiomeProbe() func(qx, qy, qz int32) string
}

// BiomeHit is where a biome search stopped.
type BiomeHit struct {
	X, Y, Z int32
	Biome   string
}

// BiomeSearch is the shape of one /locate biome search.
type BiomeSearch struct {
	// OriginX, OriginY and OriginZ are the block the search spirals out from.
	OriginX, OriginY, OriginZ int32
	// Radius is the search radius in blocks; HStep and VStep space the sampled
	// columns and heights.
	Radius, HStep, VStep int32
	// Allowed says whether a biome, by wire name, is a match.
	Allowed func(biome string) bool
}

// FindClosestBiome spirals out from the origin and returns the first sampled
// position with an allowed biome; false if the generator cannot sample, none of
// its biomes is allowed, or none is in reach.
// vanilla: BiomeSource.findClosestBiome3d
func (w *World) FindClosestBiome(s BiomeSearch) (BiomeHit, bool) {
	sampler, ok := w.gen.(BiomeSampler)
	if !ok {
		return BiomeHit{}, false
	}
	candidates := make(map[string]bool)
	for _, name := range sampler.PossibleBiomes() {
		if s.Allowed(name) {
			candidates[name] = true
		}
	}
	if len(candidates) == 0 {
		return BiomeHit{}, false
	}
	probe := sampler.NewBiomeProbe()
	sampleRadius := floorDiv32(s.Radius, s.HStep)
	ys := outFromOrigin(s.OriginY, w.MinY()+1, w.MinY()+w.Height(), s.VStep)
	var hit BiomeHit
	found := false
	spiralAround(sampleRadius, func(dx, dz int32) bool {
		bx := s.OriginX + dx*s.HStep
		bz := s.OriginZ + dz*s.HStep
		for _, by := range ys {
			if name := probe(bx>>2, by>>2, bz>>2); candidates[name] {
				hit, found = BiomeHit{X: bx, Y: by, Z: bz, Biome: name}, true
				return false
			}
		}
		return true
	})
	return hit, found
}

func floorDiv32(a, b int32) int32 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// outFromOrigin lists the heights from the clamped origin outwards, below
// then above, one step apart, within [lower, upper].
// vanilla: Mth.outFromOrigin
func outFromOrigin(origin, lower, upper, step int32) []int32 {
	clamped := min(max(origin, lower), upper)
	var out []int32
	for cursor := clamped; ; {
		dist := cursor - clamped
		if dist < 0 {
			dist = -dist
		}
		if clamped-dist < lower && clamped+dist > upper {
			break
		}
		out = append(out, cursor)
		previousWasNegative := cursor <= clamped
		canMovePositive := clamped+dist+step <= upper
		if attempted := clamped - dist - boolStep(previousWasNegative, step); previousWasNegative && canMovePositive || attempted < lower {
			cursor = clamped + dist + step
		} else {
			cursor = attempted
		}
	}
	return out
}

func boolStep(b bool, step int32) int32 {
	if b {
		return step
	}
	return 0
}

// spiralAround visits the offsets of a square spiral from (0, 0), starting east
// and turning south, until visit returns false or 4*radius legs are walked.
// vanilla: BlockPos.spiralAround
func spiralAround(radius int32, visit func(dx, dz int32) bool) {
	// East, south, west, north.
	dirs := [4][2]int32{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	x, z := int32(0), int32(1)
	legs := 4 * radius
	leg, legSize, legIndex := int32(-1), int32(0), int32(0)
	for {
		d := dirs[(leg+4)%4]
		x, z = x+d[0], z+d[1]
		if legIndex >= legSize {
			if leg >= legs {
				return
			}
			leg++
			legIndex = 0
			legSize = leg/2 + 1
		}
		legIndex++
		if !visit(x, z) {
			return
		}
	}
}

// PossibleBiomes is the one biome of the flat world.
func (g *FlatGenerator) PossibleBiomes() []string { return []string{g.biomeName} }

// NewBiomeProbe reads the flat world's one biome everywhere.
// vanilla: FixedBiomeSource.getNoiseBiome
func (g *FlatGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	return func(int32, int32, int32) string { return g.biomeName }
}

// PossibleBiomes is the biomes the nether's climate parameters name.
func (g *NetherGenerator) PossibleBiomes() []string {
	out := make([]string, len(g.biomes))
	for i, id := range g.biomes {
		out[i] = registry.Biome(id).Name()
	}
	return out
}

// NewBiomeProbe reads the nether's climate biome at a quart.
// vanilla: MultiNoiseBiomeSource.getNoiseBiome
func (g *NetherGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	return func(qx, qy, qz int32) string {
		return registry.Biome(g.biomes[terrain.NetherBiomeAt(g.state.Router, qx, qy, qz)]).Name()
	}
}

// PossibleBiomes is the End's five biomes.
func (g *EndGenerator) PossibleBiomes() []string {
	return []string{
		registry.Biome(g.theEnd).Name(), registry.Biome(g.endHighlands).Name(), registry.Biome(g.endMidlands).Name(),
		registry.Biome(g.smallEndIslands).Name(), registry.Biome(g.endBarrens).Name(),
	}
}

// NewBiomeProbe reads the End's biome at a quart.
// vanilla: TheEndBiomeSource.getNoiseBiome
func (g *EndGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	return func(qx, qy, qz int32) string {
		return registry.Biome(g.biomeForChunk(qx>>2, qz>>2)).Name()
	}
}
