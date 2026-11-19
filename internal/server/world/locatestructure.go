package world

import (
	"cmp"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// StructureLocator is a generator that can find where its structures start
// without generating chunks.
type StructureLocator interface {
	// NearestStructure returns the placement's locate position
	// and id of the nearest of the structures to the block, searching
	// radius cells of a random-spread placement. A concentric-rings
	// placement ignores the radius.
	// vanilla: ChunkGenerator.findNearestMapStructure
	NearestStructure(ids []string, x, y, z int32, radius int) (bx, bz int32, id string, ok bool)
}

// NearestStructure finds the nearest of the structures the dimension's
// generator places, and reports false when it places none of them.
func (w *World) NearestStructure(ids []string, x, y, z int32, radius int) (int32, int32, string, bool) {
	locator, ok := w.gen.(StructureLocator)
	if !ok {
		return 0, 0, "", false
	}
	return locator.NearestStructure(ids, x, y, z, radius)
}

const (
	strongholdID = "minecraft:stronghold"
	fortressID   = "minecraft:fortress"
	bastionID    = "minecraft:bastion_remnant"

	// The strongholds structure set's concentric_rings placement.
	// vanilla: StructureSets.bootstrap
	strongholdDistance = 32
	strongholdSpread   = 3
	strongholdCount    = 128
)

var (
	strongholdPreferredTag = registry.BiomeTagOf("minecraft:stronghold_biased_to")
	strongholdBiomeTag     = registry.BiomeTagOf("minecraft:has_structure/stronghold")
)

func containsID(ids []string, id string) bool {
	return slices.Contains(ids, id)
}

// strongholdRings holds the stronghold layout of a seed. A start moves to a
// preferred biome by reading a 57x57 quart square, so the starts are laid out
// at once and each is resolved the first time a search needs it.
type strongholdRings struct {
	mu sync.Mutex
	// seed lays the starts out: the level seed, 0 in a flat world.
	seed     int64
	starts   []levelgen.RingStart
	chunks   [][2]int32
	resolved []bool
	batch    int // starts resolved at once
}

// sharedRings holds each seed and preset's stronghold layout for every
// generator of them (ringsKey to *strongholdRings). The layout depends only on
// those, as the biome source does, and resolving it reads 128 squares of 57x57
// quarts: seconds of CPU a new generator for a known seed need not repeat.
var sharedRings sync.Map

// ringsKey keys sharedRings: the preset decides the climate the starts move by,
// and a flat world's biome (-1 for the noise overworld) replaces it.
type ringsKey struct {
	seed      int64
	preset    overworldPreset
	flatBiome int32
}

// ringsOf returns the seed and preset's shared stronghold layout.
func ringsOf(seed int64, preset overworldPreset) *strongholdRings {
	return sharedRingsOf(ringsKey{seed, preset, -1})
}

// ringsFor is the generator's stronghold layout for the level seed. A flat
// world lays its rings out with seed 0 whatever the level seed.
// vanilla: ChunkGeneratorStructureState.createForFlat (concentricRingsSeed 0)
func (g *OverworldGenerator) ringsFor(seed int64) *strongholdRings {
	if g.flat != nil {
		return sharedRingsOf(ringsKey{0, g.preset, g.flat.biome})
	}
	return ringsOf(seed, g.preset)
}

func sharedRingsOf(key ringsKey) *strongholdRings {
	if r, ok := sharedRings.Load(key); ok {
		return r.(*strongholdRings)
	}
	r, _ := sharedRings.LoadOrStore(key, &strongholdRings{seed: key.seed, batch: ringBatch})
	return r.(*strongholdRings)
}

// ringMoveBlocks bounds how far a ring chunk's centre moves from its start's:
// the search square's 112 blocks, the quart's 3 and half a chunk.
const ringMoveBlocks = 128

// ringBatch is how many starts one round resolves at once, one per goroutine.
const ringBatch = 4

// resolve moves the unresolved starts of the indices to their preferred
// biome, in parallel; the caller holds r.mu.
func (g *OverworldGenerator) resolve(r *strongholdRings, indices []int) {
	if b, ok := g.fixedBiome(); ok {
		preferred := b.InTag(strongholdPreferredTag)
		for _, i := range indices {
			if !r.resolved[i] {
				r.chunks[i], r.resolved[i] = r.starts[i].ResolveFixed(preferred), true
			}
		}
		return
	}
	var wg sync.WaitGroup
	for _, i := range indices {
		if r.resolved[i] {
			continue
		}
		wg.Go(func() {
			probe := g.newBiomeIDProbe()
			r.chunks[i] = r.starts[i].Resolve(func(qx, qz int32) bool {
				return probe(qx, 0, qz).InTag(strongholdPreferredTag)
			})
		})
	}
	wg.Wait()
	for _, i := range indices {
		r.resolved[i] = true
	}
}

// layOut lays the ring starts out on first use; the caller holds r.mu.
func (g *OverworldGenerator) layOut(r *strongholdRings) {
	if r.starts == nil {
		r.starts = levelgen.ConcentricRingStarts(r.seed, strongholdDistance, strongholdSpread, strongholdCount)
		r.chunks = make([][2]int32, len(r.starts))
		r.resolved = make([]bool, len(r.starts))
	}
}

// strongholdChunks resolves every ring start and returns the chunks.
func (g *OverworldGenerator) strongholdChunks() [][2]int32 {
	r := g.rings
	r.mu.Lock()
	defer r.mu.Unlock()
	g.layOut(r)
	for i := 0; i < len(r.starts); i += r.batch {
		g.resolve(r, indicesFrom(i, min(i+r.batch, len(r.starts))))
	}
	return r.chunks
}

func indicesFrom(lo, hi int) []int {
	out := make([]int, 0, hi-lo)
	for i := lo; i < hi; i++ {
		out = append(out, i)
	}
	return out
}

// nearestStronghold finds the nearest stronghold whose start chunk lies in a
// biome strongholds generate in. It resolves the ring starts nearest the block
// first and stops once no unresolved start can be closer than the best found,
// since a start moves its chunk at most ringMoveBlocks on each axis.
// vanilla: ChunkGenerator.getNearestGeneratedStructure (concentric rings)
func (g *OverworldGenerator) nearestStronghold(ids []string, x, y, z int32, accept func(string, int32, int32) bool) (int32, int32, string, bool) {
	if g.noStructures || !containsID(ids, strongholdID) {
		return 0, 0, "", false
	}
	r := g.rings
	r.mu.Lock()
	defer r.mu.Unlock()
	g.layOut(r)
	// The ring chunks are compared at (centre, 32, centre).
	dyy := float64(32-y) * float64(32-y)
	nearest := func(c [2]int32) float64 {
		dx, dz := float64(c[0]*16+8-x), float64(c[1]*16+8-z)
		return dx*dx + dyy + dz*dz
	}
	bound := func(i int) float64 {
		dx := max(math.Abs(float64(r.starts[i].X*16+8-x))-ringMoveBlocks, 0)
		dz := max(math.Abs(float64(r.starts[i].Z*16+8-z))-ringMoveBlocks, 0)
		return dx*dx + dyy + dz*dz
	}
	order := make([]int, len(r.starts))
	for i := range order {
		order[i] = i
	}
	if accept == nil {
		slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(bound(a), bound(b)) })
	}

	probe := g.newBiomeIDProbe()
	var bestX, bestZ int32
	best, bestIndex, found := 0.0, 0, false
	for len(order) > 0 {
		n := min(r.batch, len(order))
		if accept == nil && found && bound(order[0]) > best {
			break
		}
		batch := order[:n]
		order = order[n:]
		g.resolve(r, batch)
		for _, i := range batch {
			c := r.chunks[i]
			d := nearest(c)
			if found && (d > best || d == best && i > bestIndex) {
				continue
			}
			// vanilla: Structure.isValidBiome, read at the chunk's corner at y 0.
			if !probe(c[0]*4, 0, c[1]*4).InTag(strongholdBiomeTag) {
				continue
			}
			if accept != nil {
				// Reserving can generate this chunk, which reads the ring cache again.
				accepted := func() bool {
					r.mu.Unlock()
					defer r.mu.Lock()
					return accept(strongholdID, c[0], c[1])
				}()
				if !accepted {
					continue
				}
			}
			bestX, bestZ, best, bestIndex, found = c[0]*16, c[1]*16, d, i, true
		}
	}
	return bestX, bestZ, strongholdID, found
}

// NearestStructure finds the nearest fortress, bastion remnant, nether fossil
// or ruined portal, scanning every wanted placement ring by ring.
// vanilla: ChunkGenerator.findNearestMapStructure
func (g *NetherGenerator) NearestStructure(ids []string, x, _, z int32, radius int) (int32, int32, string, bool) {
	if g.noStructures {
		return 0, 0, "", false
	}
	type placement struct {
		spacing, separation, salt int32
		hit                       func(cx, cz int32) string
	}
	var placements []placement
	wantFortress, wantBastion := containsID(ids, fortressID), containsID(ids, bastionID)
	if wantFortress || wantBastion {
		placements = append(placements, placement{levelgen.NetherComplexSpacing, levelgen.NetherComplexSeparation, levelgen.NetherComplexSalt,
			func(cx, cz int32) string {
				if g.complexIsFortress(cx, cz) {
					if wantFortress {
						return fortressID
					}
				} else if wantBastion {
					return bastionID
				}
				return ""
			}})
	}
	if containsID(ids, netherFossilID) {
		d := netherFossils()
		placements = append(placements, placement{d.spacing, d.separation, d.salt, func(cx, cz int32) string {
			if s := g.fossilInCell(floorDiv(cx, d.spacing), floorDiv(cz, d.spacing)); s != nil && g.resolved(s) {
				return netherFossilID
			}
			return ""
		}})
	}
	if containsID(ids, ruinedPortalID) {
		placements = append(placements, placement{ruinedPortalSpacing, ruinedPortalSeparation, ruinedPortalSalt, func(cx, cz int32) string {
			if g.ruinedPortalInCell(floorDiv(cx, ruinedPortalSpacing), floorDiv(cz, ruinedPortalSpacing)) != nil {
				return ruinedPortalID
			}
			return ""
		}})
	}
	for r := int32(0); r <= int32(radius); r++ {
		var best int64
		var bestX, bestZ int32
		var bestID string
		for _, p := range placements {
			var id string
			cx, cz, ok := levelgen.RandomSpreadRing(g.seed, p.spacing, p.separation, p.salt, x>>4, z>>4, r, func(sx, sz int32) bool {
				id = p.hit(sx, sz)
				return id != ""
			})
			if !ok {
				continue
			}
			dx, dz := int64(cx*16-x), int64(cz*16-z)
			if d := dx*dx + dz*dz; bestID == "" || d < best {
				best, bestX, bestZ, bestID = d, cx*16, cz*16, id
			}
		}
		if bestID != "" {
			return bestX, bestZ, bestID, true
		}
	}
	return 0, 0, "", false
}

// NearestStructure searches the End cities' triangular random-spread placement.
// vanilla: ChunkGenerator.getNearestGeneratedStructure
func (g *EndGenerator) NearestStructure(ids []string, x, _, z int32, radius int) (int32, int32, string, bool) {
	if !containsID(ids, endCityID) {
		return 0, 0, "", false
	}
	set := levelgen.StructureSetNamed("minecraft:end_cities")
	p := set.Placement
	for r := int32(0); r <= int32(radius); r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if dx != -r && dx != r && dz != -r && dz != r {
					continue
				}
				cx, cz := p.PotentialChunk(g.seed, (x>>4)+p.Spacing*dx, (z>>4)+p.Spacing*dz)
				if g.endCityInCell(floorDiv(cx, p.Spacing), floorDiv(cz, p.Spacing)) != nil {
					return cx * 16, cz * 16, endCityID, true
				}
			}
		}
	}
	return 0, 0, "", false
}
