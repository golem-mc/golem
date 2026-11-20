package world

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// StructurePlacement is how PlaceStructure ended.
type StructurePlacement int

const (
	// StructurePlaced means the structure's blocks were computed.
	StructurePlaced StructurePlacement = iota
	// StructureNoStart means the structure has no generator here or found no
	// point.
	StructureNoStart
	// StructureUnloaded means a chunk the structure reaches is not loaded.
	StructureUnloaded
)

// PlaceStructure builds the start of structure id in the chunk holding
// (x, z), on the surface of the loaded world and in any biome, and returns the
// blocks its pieces write and their extras without writing them.
// vanilla: PlaceCommand.placeStructure
func (w *World) PlaceStructure(id string, seed int64, x, z int32) ([]BlockChange, PlacementExtras, StructurePlacement) {
	if w.MinY() != worldMinY || w.Height() != worldHeight {
		return nil, PlacementExtras{}, StructureNoStart
	}
	g := &OverworldGenerator{}
	if og, ok := w.gen.(*OverworldGenerator); ok {
		g.preset = og.preset
	}
	g.setSeed(w.SeedValue())
	typ := g.structures.typeOf(id)
	if typ == nil {
		return nil, PlacementExtras{}, StructureNoStart
	}
	g.liveHeight = w.SurfaceHeight
	start := g.buildStart(id, typ, x>>4, z>>4, false)
	if start == nil {
		return nil, PlacementExtras{}, StructureNoStart
	}
	return w.placeStart(start, seed)
}

// placeStart writes a grown start's pieces and returns the changes and their
// extras without applying them; the chunks it reaches must be loaded. A start wider than one
// decoration region is written in tiles of 3x3 chunks, each in a region of its
// own: a piece writes only inside the chunk being decorated, so the tiles do
// not overlap.
func (w *World) placeStart(start *structureStart, seed int64) ([]BlockChange, PlacementExtras, StructurePlacement) {
	minCX, minCZ := start.box.minX>>4, start.box.minZ>>4
	maxCX, maxCZ := start.box.maxX>>4, start.box.maxZ>>4
	for sx := minCX; sx <= maxCX; sx++ {
		for sz := minCZ; sz <= maxCZ; sz++ {
			if w.chunkIfLoaded(ChunkPos{X: sx, Z: sz}) == nil {
				return nil, PlacementExtras{}, StructureUnloaded
			}
		}
	}
	first := start.pieces[0].bounds()
	ref := [3]int32{first.minX + (first.maxX-first.minX+1)/2, first.minY, first.minZ + (first.maxZ-first.minZ+1)/2}
	const tile = decorRegionSpan - 2
	var changes []BlockChange
	var extras PlacementExtras
	for tx := minCX; tx <= maxCX; tx += tile {
		for tz := minCZ; tz <= maxCZ; tz += tile {
			endX, endZ := min(tx+tile-1, maxCX), min(tz+tile-1, maxCZ)
			center := w.chunkIfLoaded(ChunkPos{X: (tx + endX) >> 1, Z: (tz + endZ) >> 1})
			r := newDecorRegion(&liveTerrain{w: w, chunks: map[ChunkPos]*shapedTerrain{}}, center, worldMinY, worldHeight)
			r.worldSeed = w.SeedValue()
			r.rng.SetSeed(seed)
			for sx := tx; sx <= endX; sx++ {
				for sz := tz; sz <= endZ; sz++ {
					r.nx, r.nz = sx, sz
					chunk := jbox{sx * 16, worldMinY, sz * 16, sx*16 + 15, worldMinY + worldHeight, sz*16 + 15}
					for _, p := range start.pieces {
						if p.bounds().intersects(chunk) {
							p.place(r, chunk, ref)
						}
					}
					if a, ok := start.typ.(structureAfterPlacer); ok {
						a.afterPlace(r, chunk, start)
					}
				}
			}
			changes = append(changes, r.changesFrom(w, center, &extras)...)
		}
	}
	return changes, extras, StructurePlaced
}

// GenerateJigsaw grows a jigsaw assembly from the pool at (x, y, z), starting
// at the first jigsaw block named target, and returns the blocks it writes and
// their extras without writing them; keep leaves the jigsaw blocks in place.
// vanilla: JigsawPlacement.generateJigsaw
func (w *World) GenerateJigsaw(pool, target string, maxDepth int, seed int64, x, y, z int32, keep bool) ([]BlockChange, PlacementExtras, StructurePlacement) {
	if w.MinY() != worldMinY || w.Height() != worldHeight {
		return nil, PlacementExtras{}, StructureNoStart
	}
	startPool, pools, err := jigsawPoolGraph(pool)
	if err != nil {
		return nil, PlacementExtras{}, StructureNoStart
	}
	def := &jigsawStructure{startPool: startPool, pools: pools, maxDepth: maxDepth, maxDistance: 128, startJigsaw: target}
	// The loaded world answers heights, so no overworld generator (and its noise setup) is built.
	freeHeight := func(x, z int32) int32 { return max(w.SurfaceHeight(x, z)+1, levelgen.OverworldSeaLevel) }
	rng := levelgen.NewLegacyRandom(seed)
	start, expand := planJigsawAt(def, rng, [3]int32{x, y, z}, map[string]string{}, worldMinY, worldMinY+worldHeight-1, freeHeight, keep)
	if start == nil {
		return nil, PlacementExtras{}, StructureNoStart
	}
	expand()
	grown := &structureStart{chunkX: start.chunkX, chunkZ: start.chunkZ, box: start.box}
	for _, p := range start.pieces {
		grown.pieces = append(grown.pieces, p)
	}
	return w.placeStart(grown, seed)
}
