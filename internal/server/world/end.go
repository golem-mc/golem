package world

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The end generator: the End router's island density as end stone,
// end_features.go's features on top.
//
// Features retain their proto chunks between neighbouring generation requests.

// The ten pillars stand endPillarRing blocks out; the arrival platform is at
// endArrival*, where vanilla's arrivals land.
const (
	endPillarRing  = 42
	endPillarCount = 10
	endArrivalX    = 100
	endArrivalY    = 48
	endArrivalZ    = 0

	// endSeaLevel is the sea level the end's noise settings carry.
	// vanilla: NoiseGeneratorSettings.end
	endSeaLevel = 0
)

// endPillar is one ring pillar, fixed per generator: position, width and top
// height.
type endPillar struct {
	x, z   int32 // the centre of its footprint
	radius int32
	top    int32 // the y of the bedrock cap, the pillar's highest solid block
	// guarded pillars wear an iron-bars cage round the crystal.
	guarded bool
}

// EndGenerator generates the seeded End and retains unfinished decorations.
type EndGenerator struct {
	decorState
	seed  int64
	state *terrain.RandomState
	// zoomSeed seeds the biome zoom that feature placement reads biomes
	// through.
	zoomSeed int64
	cache    endTerrainCache
	biomes   endBiomeCache
	cities   sync.Map // [2]int32 to a once-resolved city cell

	// The ten pillars of the ring, decided by the seed once.
	pillars [endPillarCount]endPillar

	// The terrain's block states, resolved once so generation never touches the
	// registry again.
	endStoneState uint16
	obsidianState uint16
	bedrockState  uint16
	fireState     uint16
	chorusStates
	// cageBars holds iron-bars states indexed by north, south, west, east
	// connections as bits 0..3.
	cageBars [16]uint16

	// The biomes the generator assigns, as registry IDs for the wire.
	theEnd          int32
	endHighlands    int32
	endMidlands     int32
	smallEndIslands int32
	endBarrens      int32
}

// NewEndGenerator returns an End generator for seed, panicking if registry data is missing.
func NewEndGenerator(seed int64) *EndGenerator {
	g := &EndGenerator{seed: seed, state: terrain.NewRandomState(terrain.End, seed), zoomSeed: levelgen.BiomeZoomSeed(seed)}

	// The ring's ten spots are fixed; the seed only deals the sizes 0..9 onto
	// them.
	// vanilla: EndSpikeFeature.getSpikesForLevel, EndSpikeFeature.SpikeCacheLoader.load
	g.pillars = endSpikes(seed)

	g.endStoneState = registry.BlockEndStone.DefaultState()
	g.obsidianState = registry.BlockObsidian.DefaultState()
	g.bedrockState = registry.BlockBedrock.DefaultState()
	g.fireState = registry.BlockFire.DefaultState()
	g.chorusStates = newChorusStates()
	g.cageBars = spikeCageBars

	mustBiome := func(name string) int32 {
		return registry.MustID("minecraft:worldgen/biome", name)
	}
	g.theEnd = mustBiome("minecraft:the_end")
	g.endHighlands = mustBiome("minecraft:end_highlands")
	g.endMidlands = mustBiome("minecraft:end_midlands")
	g.smallEndIslands = mustBiome("minecraft:small_end_islands")
	g.endBarrens = mustBiome("minecraft:end_barrens")
	return g
}

// SeaLevel returns the end's noise-settings sea level.
func (g *EndGenerator) SeaLevel() int32 {
	return endSeaLevel
}

// endSpikes draws the ten pillars for a seed: fixed spots, sizes 0..9 shuffled
// onto them, so every height 76..103 and radius 2..5 appears once in every
// world. Sizes 1 and 2 are caged.
// vanilla: EndSpikeFeature.getSpikesForLevel, EndSpikeFeature.SpikeCacheLoader.load
func endSpikes(seed int64) [endPillarCount]endPillar {
	// getSpikesForLevel reads one nextLong off the level seed and keys the
	// spike cache with its low 16 bits.
	rng := newLegacyRandom(seed)
	key := rng.nextLong() & 0xFFFF

	// Shuffle sizes 0..9 top-down, as Collections.shuffle does with
	// java.util.Random.
	shuffle := newLegacyRandom(key)
	var sizes [endPillarCount]int32
	for i := range sizes {
		sizes[i] = int32(i)
	}
	for i := int32(endPillarCount); i > 1; i-- {
		j := shuffle.nextInt(i)
		sizes[i-1], sizes[j] = sizes[j], sizes[i-1]
	}

	// 0.3141592653589793 is π/10 as vanilla spells it, so the product rounds
	// the same. The floor, not a round, puts the west pillar at (-42, -1):
	// 42·sin lands just below zero there.
	var pillars [endPillarCount]endPillar
	for i := range pillars {
		angle := 2 * (-math.Pi + 0.3141592653589793*float64(i))
		size := sizes[i]
		pillars[i] = endPillar{
			x:      int32(math.Floor(endPillarRing * math.Cos(angle))),
			z:      int32(math.Floor(endPillarRing * math.Sin(angle))),
			radius: 2 + size/3,
			top:    76 + 3*size,

			guarded: size == 1 || size == 2,
		}
	}
	return pillars
}

// Generate fills a chunk with the end: island terrain, features the chunk and
// its neighbours place in it, and its biome.
func (g *EndGenerator) Generate(c *Chunk) {
	wcx, wcz := c.x*sectionSize, c.z*sectionSize
	t := g.terrainOf(c.x, c.z)
	for y := range int32(endHeight) {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				if t.solid(x, y, z) {
					c.SetBlock(x, endMinY+y, z, g.endStoneState)
				}
			}
		}
	}
	r := newEndRegion(g)
	r.protos = map[ChunkPos]*decorProto{}
	g.decorateAround(ChunkPos{X: c.x, Z: c.z}, r.decorateChunk, func(own *decorProto) {
		var dummies []dummyEntity
		for y := range int32(endHeight) {
			if own.blocks[y>>4] == nil {
				continue
			}
			for z := range int32(sectionSize) {
				for x := range int32(sectionSize) {
					if state, ok := own.get(x, y, z); ok {
						c.SetBlock(x, endMinY+y, z, state)
						dummies = appendDummy(dummies, wcx+x, endMinY+y, wcz+z, state)
					}
				}
			}
		}
		for _, entity := range own.entities {
			entity.place(c, endMinY)
		}
		c.promoteDummies(dummies)
	})
	c.SetBiome(g.biomeForChunk(c.x, c.z))
	g.saveStructureStarts(c)
}

// podiumTorches are the wall torches 2 above the origin around the column,
// facing away from it.
var podiumTorches = [...]struct {
	dx, dz int32
	facing string
}{{0, -1, "north"}, {0, 1, "south"}, {-1, 0, "west"}, {1, 0, "east"}}

// GeneratedEntities returns the chunk's pillar crystals and End city entities.
// vanilla: EndSpikeFeature.placeSpike
func (g *EndGenerator) GeneratedEntities(pos ChunkPos) []nbt.Compound {
	out := g.endCityEntities(pos)
	var rng *levelgen.WorldgenRandom
	for _, p := range g.pillars {
		if floorDiv(p.x, sectionSize) != pos.X || floorDiv(p.z, sectionSize) != pos.Z {
			continue
		}
		// The yaw is the spike feature's one draw per pillar.
		if rng == nil {
			rng = levelgen.NewWorldgenRandom(0)
			seed := rng.SetDecorationSeed(g.seed, pos.X*sectionSize, pos.Z*sectionSize)
			rng.SetFeatureSeed(seed, slotEndSpike.Index, slotEndSpike.Step)
		}
		yaw := rng.NextFloat() * 360
		id := uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "end_crystal/%d/%d/%d", g.seed, p.x, p.z))
		out = append(out, nbt.Compound{
			"id": nbt.String("minecraft:end_crystal"),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(float64(p.x) + 0.5), nbt.Double(p.top + 1), nbt.Double(float64(p.z) + 0.5),
			}},
			"Motion": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(0), nbt.Double(0), nbt.Double(0),
			}},
			"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
				nbt.Float(yaw), nbt.Float(0),
			}},
			"UUID":       nbt.UUIDToIntArray(id),
			"ShowBottom": nbt.Byte(1),
		})
	}
	return out
}

// biomeForChunk is the chunk's biome; the end's biome source reads one erosion
// value per chunk, so every quart agrees.
// vanilla: TheEndBiomeSource.getNoiseBiome
func (g *EndGenerator) biomeForChunk(cx, cz int32) int32 {
	switch g.chunkBiome(cx, cz) {
	case levelgen.EndHighlands:
		return g.endHighlands
	case levelgen.EndMidlands:
		return g.endMidlands
	case levelgen.SmallEndIslands:
		return g.smallEndIslands
	case levelgen.EndBarrens:
		return g.endBarrens
	}
	return g.theEnd
}

// EndArrival is where arrivals stand: one above vanilla's obsidian platform at
// (100, 48, 0).
func EndArrival() SpawnPos {
	return SpawnPos{X: endArrivalX, Y: endArrivalY + 1, Z: endArrivalZ}
}

// EndArrivalPlatform returns the changes building the arrival platform: 5x5
// obsidian at y=48 centred on (100, 0) with three blocks of air above each, for
// a server that places it on arrival; the generator bakes the same blocks in.
func EndArrivalPlatform() []BlockChange {
	changes := make([]BlockChange, 0, 100)
	obsidian := registry.BlockObsidian.DefaultState()
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			changes = append(changes, BlockChange{X: endArrivalX + x, Y: endArrivalY, Z: endArrivalZ + z, State: obsidian})
			for dy := int32(1); dy <= 3; dy++ {
				changes = append(changes, BlockChange{X: endArrivalX + x, Y: endArrivalY + dy, Z: endArrivalZ + z, State: airState})
			}
		}
	}
	return changes
}

// EndSpikeBoxes returns the seed's spike census boxes, spanning the build height.
// vanilla: EndSpikeFeature.EndSpike.topBoundingBox
func EndSpikeBoxes(seed int64) [endPillarCount]Box {
	var boxes [endPillarCount]Box
	for i, p := range endSpikes(seed) {
		boxes[i] = Box{MinX: float64(p.x - p.radius), MinY: -2032, MinZ: float64(p.z - p.radius), MaxX: float64(p.x + p.radius), MaxY: 2031, MaxZ: float64(p.z + p.radius)}
	}
	return boxes
}

// EndPodiumMatches tests the required bedrock, leaving all other cells unconstrained.
// vanilla: EnderDragonFight.exitPortalPattern
func EndPodiumMatches(ox, oy, oz int32, at func(int32, int32, int32) uint16) bool {
	matched := true
	PlaceEndPodium(ox, oy, oz, false, func(x, y, z int32, state uint16) {
		if state == bedrockLayoutState && matched {
			matched = registry.BlockOf(at(x, y, z)) == registry.BlockBedrock
		}
	})
	return matched
}

// PlaceEndPodium builds the active or inactive exit portal.
// vanilla: EndPodiumFeature.place
func PlaceEndPodium(ox, oy, oz int32, active bool, set func(int32, int32, int32, uint16)) {
	for dx := int32(-4); dx <= 4; dx++ {
		for dz := int32(-4); dz <= 4; dz++ {
			for y := oy - 1; y <= oy+3; y++ {
				dy := y - oy
				d2 := float64(dx*dx + dy*dy + dz*dz)
				inside := d2 < 2.5*2.5
				if !inside && d2 >= 3.5*3.5 {
					continue
				}
				switch {
				case y < oy && inside:
					set(ox+dx, y, oz+dz, bedrockLayoutState)
				case y < oy:
					set(ox+dx, y, oz+dz, registry.BlockEndStone.DefaultState())
				case y > oy:
					set(ox+dx, y, oz+dz, airState)
				case !inside:
					set(ox+dx, y, oz+dz, bedrockLayoutState)
				case active:
					set(ox+dx, y, oz+dz, registry.BlockEndPortal.DefaultState())
				default:
					set(ox+dx, y, oz+dz, airState)
				}
			}
		}
	}
	for y := range int32(4) {
		set(ox, oy+y, oz, bedrockLayoutState)
	}
	for i, f := range podiumTorches {
		set(ox+f.dx, oy+2, oz+f.dz, podiumTorchStates[i])
	}
}

var podiumTorchStates = func() [4]uint16 {
	var states [4]uint16
	for i, f := range podiumTorches {
		states[i] = registry.MustState("minecraft:wall_torch", map[string]string{"facing": f.facing})
	}
	return states
}()

var spikeCageBars = func() [16]uint16 {
	var states [16]uint16
	for i := range states {
		sides := map[string]string{}
		for bit, name := range [...]string{"north", "south", "west", "east"} {
			sides[name] = strconv.FormatBool(i&(1<<bit) != 0)
		}
		states[i], _ = registry.FindState("minecraft:iron_bars", sides)
	}

	return states
}()
