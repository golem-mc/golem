package world

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/world/terrain"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// maskCacheSize bounds the per-chunk biome sets kept for decoration.
const maskCacheSize = 8192

// overworldPlaced is a placed feature: its placement modifiers, ending in the
// biome filter, and the feature they feed.
type overworldPlaced struct {
	feature terrain.OverworldFeature
	mods    []placementMod
	place   func(r *decorRegion, p [3]int32)
}

var overworldAirBlocks = blocksNamed("minecraft:air", "minecraft:cave_air", "minecraft:void_air")

func (g *OverworldGenerator) cachedTerrain(cx, cz int32) *shapedTerrain {
	return g.cache.cached(ChunkPos{X: cx, Z: cz})
}

func (g *OverworldGenerator) terrainOf(cx, cz int32, shaped *Chunk) *shapedTerrain {
	return g.cache.of(ChunkPos{X: cx, Z: cz}, shaped, g.shapers, func(c *Chunk) { g.shapeTerrain(c) })
}

func (g *OverworldGenerator) decor() *decorState { return &g.decorState }

func (g *OverworldGenerator) isAir(state uint16) bool {
	return overworldAirBlocks[registry.BlockOf(state)]
}

// biomeIndex is the biome a feature's filter reads at a block: the zoomed
// quart's stored biome cell. Resampling skips the chunk fill's search history.
// vanilla: BiomeManager.getBiome, LevelReader.getNoiseBiome
func (g *OverworldGenerator) biomeIndex(r *decorRegion, x, y, z int32) uint {
	qx, qy, qz := levelgen.ZoomedQuart(g.zoomSeed, x, y, z)
	return uint(r.biomeCells.cell(qx, qy, qz))
}

// biomeMask is the set of biomes in a chunk's cells.
// Generate stores its own chunk's mask, so only neighbours are computed here.
// vanilla: ChunkAccess.collectBiomesInPalette
func (g *OverworldGenerator) biomeMask(cx, cz int32) uint64 {
	pos := ChunkPos{X: cx, Z: cz}
	g.masksMu.Lock()
	m, ok := g.masks[pos]
	g.masksMu.Unlock()
	if ok {
		return m
	}
	m = g.biomeCellsOf(cx, cz).mask
	g.masksMu.Lock()
	g.storeMask(pos, m)
	g.masksMu.Unlock()
	return m
}

// storeMask caches a chunk's biome set; called under masksMu.
func (g *OverworldGenerator) storeMask(pos ChunkPos, m uint64) {
	if len(g.masks) >= maskCacheSize {
		clear(g.masks)
	}
	g.masks[pos] = m
}

// decorateOverworld decorates the not-yet-decorated chunks around c, then
// writes what landed in c.
func (g *OverworldGenerator) decorateOverworld(c *Chunk) {
	r := newDecorRegion(g, c, worldMinY, worldHeight)
	r.biomeCells = &cellReader{g: g}
	r.decorateAround(r.decorateOverworld, r.finishOverworld)
}

// DecorateInOrder runs each chunk's features once in seq order, without
// finishing chunks, so later Generate calls can consume the recorded results.
// vanilla: ChunkStatusTasks.generateFeatures, ChunkGenerator.applyBiomeDecoration
func (g *OverworldGenerator) DecorateInOrder(seq []ChunkPos) {
	for _, pos := range seq {
		c := newChunk(pos.X, pos.Z)
		g.shape(c)
		r := newDecorRegion(g, c, worldMinY, worldHeight)
		r.biomeCells = &cellReader{g: g}
		g.claimAll()
		r.decorateChunk(pos, r.decorateOverworld)
		g.releaseAll()
	}
}

// PrimeLightSources captures the columns before INITIALIZE_LIGHT waits on the
// light engine. Writes while that future is pending do not update the sources.
// vanilla: ChunkStatusTasks.initializeLight, ProtoChunk.setBlockState
func (g *OverworldGenerator) PrimeLightSources(pos ChunkPos) {
	g.claimAll()
	defer g.releaseAll()
	p := g.protos.lookup(pos)
	if p == nil {
		return
	}
	t := g.terrainOf(pos.X, pos.Z, nil)
	sources := new([256]int32)
	get := func(x, y, z int32) uint16 {
		if state, ok := p.get(x, y-worldMinY, z); ok {
			return state
		}
		return t.blockAt(x&15, y, z&15)
	}
	for z := range int32(16) {
		for x := range int32(16) {
			sources[z*16+x] = skySourceFloor(get, x, z, worldMinY, worldHeight)
		}
	}
	g.lightReplay[pos] = decorLight{sources: sources}
}

// ReplayedSkyLight is the sky light vanilla's engine computes at a cell from
// the replayed column floors over the blocks get returns: 15 at or above the
// floor, else the best level spread from a floor within 14 steps. It must not
// run concurrently with decoration or light replay.
// vanilla: ChunkSkyLightSources, SkyLightEngine
func (g *OverworldGenerator) ReplayedSkyLight(get func(x, y, z int32) uint16, x, y, z int32) uint8 {
	const reach = 14 // a source of 15 loses at least one a step
	floors := map[[2]int32]int32{}
	isSource := func(cx, cy, cz int32) bool {
		key := [2]int32{cx, cz}
		floor, ok := floors[key]
		if !ok {
			floor = g.replayedSkyFloor(get, cx, cz, worldMinY, worldHeight)
			floors[key] = floor
		}
		return cy >= floor
	}
	if isSource(x, y, z) {
		return 15
	}
	type cell = [3]int32
	inReach := func(c cell) bool {
		return c[1] >= worldMinY && abs32(c[0]-x)+abs32(c[1]-y)+abs32(c[2]-z) <= reach
	}
	levels := map[cell]uint8{}
	var buckets [16][]cell
	for dx := int32(-reach); dx <= reach; dx++ {
		for dz := int32(-reach); dz <= reach; dz++ {
			rest := reach - abs32(dx) - abs32(dz)
			for dy := -rest; dy <= rest; dy++ {
				c := cell{x + dx, y + dy, z + dz}
				if inReach(c) && isSource(c[0], c[1], c[2]) {
					levels[c] = 15
					buckets[15] = append(buckets[15], c)
				}
			}
		}
	}
	for v := uint8(15); v > 1; v-- {
		for i := 0; i < len(buckets[v]); i++ {
			c := buckets[v][i]
			if levels[c] != v {
				continue
			}
			from := get(c[0], c[1], c[2])
			for d, o := range dirDelta {
				n := cell{c[0] + o[0], c[1] + o[1], c[2] + o[2]}
				if !inReach(n) {
					continue
				}
				if nv := spread(v, d, from, get(n[0], n[1], n[2])); nv > levels[n] {
					levels[n] = nv
					buckets[nv] = append(buckets[nv], n)
				}
			}
		}
	}
	return levels[cell{x, y, z}]
}

// ReplayLight publishes a completed light-engine update, including pending
// section-status writes. Callbacks run after swapSectionMap.
// vanilla: ThreadedLevelLightEngine.runUpdate
func (g *OverworldGenerator) ReplayLight(pos ChunkPos, lit bool) {
	g.claimAll()
	defer g.releaseAll()
	state := g.lightReplay[pos]
	state.lit = lit
	state.initialized = true
	g.lightReplay[pos] = state
	g.lightDirty[pos] = true
	for pos := range g.lightDirty {
		p := g.protos.lookup(pos)
		if p == nil {
			continue
		}
		t := g.terrainOf(pos.X, pos.Z, nil)
		state := g.lightReplay[pos]
		state.top, state.occupied = decorTopSection(p, t, worldMinY, worldHeight)
		g.lightReplay[pos] = state
	}
	clear(g.lightDirty)
}

// finishOverworld copies the chunk's proto into it and hands it generated ticks.
func (r *decorRegion) finishOverworld(own *decorProto) {
	c := r.center
	r.reconnectShapes(own)
	// Aquifer marks are the only entries so far; vanilla's post-processing re-shapes them.
	// vanilla: LevelChunk.postProcessGeneration, NoiseBasedChunkGenerator.doFill
	for _, k := range c.pendingGenerated {
		x, y, z := c.x*16+k[0], k[1], c.z*16+k[2]
		if registry.BlockOf(r.get(x, y, z)).InTag(registry.TagIDSpeleothems) {
			r.templateShape(x, y, z)
		}
	}
	var dummies []dummyEntity
	for y := range int32(worldHeight) {
		if own.blocks[y>>4] == nil {
			continue
		}
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				if s, ok := own.get(x, y, z); ok {
					c.SetBlock(x, y+worldMinY, z, s)
					dummies = appendDummy(dummies, c.x*sectionSize+x, y+worldMinY, c.z*sectionSize+z, s)
				}
			}
		}
	}
	for _, k := range own.sources {
		c.scheduleGenerated(k[0], k[1]+worldMinY, k[2])
	}
	var seed int64
	if g, ok := r.src.(*OverworldGenerator); ok {
		seed = g.seed
	}
	for _, e := range own.entities {
		if be, ent := e.resolve(c.x, c.z, worldMinY, seed); ent != nil {
			c.generatedEntities = append(c.generatedEntities, ent)
		} else {
			c.placeGeneratedBlockEntity(be)
		}
	}
	c.promoteDummies(dummies)
}

// hiveEntry is the carried beehive compound for a decorated bee nest at
// (x, y, z); packed holds its bees as protoEntity.lootSeed does.
// vanilla: BeehiveDecorator, BeehiveBlockEntity.storeBee, BlockEntity.saveWithoutMetadata
func hiveEntry(x, y, z int32, packed int64) nbt.Compound {
	bees := make([]nbt.Tag, packed>>30)
	for i := range bees {
		bees[i] = nbt.Compound{
			"entity_data":       nbt.Compound{"id": nbt.String("minecraft:bee")},
			"ticks_in_hive":     nbt.Int(packed >> (10 * i) & 1023),
			"min_ticks_in_hive": nbt.Int(600),
		}
	}
	return nbt.Compound{
		"id": nbt.String("minecraft:beehive"),
		"x":  nbt.Int(x), "y": nbt.Int(y), "z": nbt.Int(z),
		"bees":       nbt.List{Elem: nbt.TypeCompound, Items: bees},
		"components": nbt.Compound{},
	}
}

// decorateOverworld runs chunk (nx, nz)'s structures and placed features in
// step and index order, a step's structures first. A feature runs when a biome carrying it is among the surrounding
// chunks; each placement is then filtered on its own position's biome.
// vanilla: ChunkGenerator.applyBiomeDecoration
func (r *decorRegion) decorateOverworld(nx, nz int32) {
	g := r.src.(*OverworldGenerator)
	r.nx, r.nz = nx, nz
	r.region = nil
	r.worldSeed = g.seed
	// vanilla: ChunkGenerator.applyBiomeDecoration creates a fresh stream per
	// chunk; setSeed alone retains WorldgenRandom's cached Gaussian.
	r.rng = levelgen.NewWorldgenRandom(0)
	var possible uint64
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			possible |= g.biomeMask(nx+dx, nz+dz)
		}
	}
	ox, oz := nx*sectionSize, nz*sectionSize
	seed := r.rng.SetDecorationSeed(g.seed, ox, oz)
	var reaching []*structureStart
	g.eachStructureReaching(nx, nz, func(s *structureStart) { reaching = append(reaching, s) })
	step := int32(-1)
	for i := range g.placed {
		f := &g.placed[i]
		for ; step < f.feature.Slot.Step; step++ {
			r.placeStructures(reaching, seed, step+1, nx, nz)
		}
		if f.feature.Biomes&possible == 0 {
			continue
		}
		r.rng.SetFeatureSeed(seed, f.feature.Slot.Index, f.feature.Slot.Step)
		r.walk(f.mods, [3]int32{ox, worldMinY, oz}, f.place)
	}
	for ; step < levelgen.DecorationSteps-1; step++ {
		r.placeStructures(reaching, seed, step+1, nx, nz)
	}
}

// initOverworldFeatures builds the placed features that are ported, in
// terrain.OverworldFeatures order; a feature without an entry here is not
// placed, though its slot still counts in the order.
func (g *OverworldGenerator) initOverworldFeatures() {
	g.placed = g.placed[:0]
	features := g.overworldFeatures()
	if g.flat != nil {
		features = g.flat.overworldFeatures()
	}
	const top = worldMinY + worldHeight - 1
	in := func(names ...string) func(uint16) bool {
		t := blocksNamed(names...)
		return func(s uint16) bool { return t[registry.BlockOf(s)] }
	}
	rock := []string{
		"minecraft:stone", "minecraft:granite", "minecraft:diorite", "minecraft:andesite",
		"minecraft:deepslate", "minecraft:tuff", "minecraft:calcite", "minecraft:dirt",
	}
	frozen := []string{"minecraft:snow_block", "minecraft:powder_snow", "minecraft:packed_ice"}
	spring := func(fluid uint16, valid func(uint16) bool) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) { r.placeSpring(fluid, p, valid, true, 4, 1) }
	}
	lavaHeight := heightVeryBiasedMod(worldMinY, top-8, 8)
	// def is a feature's placement modifiers short of the biome filter, and
	// what they feed.
	type def struct {
		mods  []placementMod
		place func(*decorRegion, [3]int32)
	}
	springDef := func(count int32, height placementMod, place func(*decorRegion, [3]int32)) def {
		return def{[]placementMod{countMod(count), inSquareMod, height}, place}
	}
	climateAt := func(r *decorRegion, x, y, z int32) registry.BiomeClimate {
		return g.biomeClimates[r.src.biomeIndex(r, x, y, z)]
	}
	defs := map[string]def{
		"minecraft:spring_water":       springDef(25, heightUniformMod(worldMinY, 192), spring(g.waterState, in(append(rock[:len(rock):len(rock)], frozen...)...))),
		"minecraft:spring_lava":        springDef(20, lavaHeight, spring(g.lavaState, in(rock...))),
		"minecraft:spring_lava_frozen": springDef(20, lavaHeight, spring(g.lavaState, in(frozen...))),
		"minecraft:freeze_top_layer":   {nil, func(r *decorRegion, p [3]int32) { r.placeSnowAndFreeze(climateAt, p) }},
	}
	underground := g.undergroundFeatures()
	for name, d := range localDefs() {
		defs[name] = d
	}
	for _, f := range features {
		if t, ok := treeFeature(f.Name, f.Biomes); ok {
			g.placed = append(g.placed, overworldPlaced{feature: f, mods: t.mods, place: func(r *decorRegion, p [3]int32) { t.place(r, p) }})
			continue
		}
		if v, ok := compileJSONPlaced(f); ok {
			g.placed = append(g.placed, v)
			continue
		}
		d, ok := defs[f.Name]
		if u, found := underground[f.Name]; found {
			d, ok = def(u), true
		}
		if !ok {
			if v, ok := compileJSONPlaced(f); ok {
				g.placed = append(g.placed, v)
			}
			continue
		}
		g.placed = append(g.placed, overworldPlaced{
			feature: f,
			mods:    append(d.mods[:len(d.mods):len(d.mods)], biomeMod(f.Biomes)),
			place:   d.place,
		})
	}
	if g.flat != nil {
		g.placed = append(g.placed, g.flat.fills(features)...)
	}
}

// overworldFeatures is terrain.OverworldFeatures as the biome source orders
// them: a single-biome world's are plains' own, indexed within each step among
// those alone, which reseeds every feature after one plains lacks.
// vanilla: ChunkGenerator.featuresPerStep, FeatureSorter.buildFeaturesPerStep
func (g *OverworldGenerator) overworldFeatures() []terrain.OverworldFeature {
	if g.preset != presetSingleBiome {
		return terrain.OverworldFeatures
	}
	var out []terrain.OverworldFeature
	next := map[int32]int32{}
	for _, f := range terrain.OverworldFeatures {
		if f.Biomes&(1<<g.singleBiome()) == 0 {
			continue
		}
		f.Slot.Index = next[f.Slot.Step]
		next[f.Slot.Step]++
		out = append(out, f)
	}
	return out
}

// cartTag is the compound of a chest minecart a mineshaft stood at
// (x, y, z). The UUID is derived from the cart's place; vanilla draws a random
// one.
// vanilla: MineshaftPieces.MineShaftCorridor.createChest
func cartTag(seed int64, x, y, z int32, cart nbt.Compound) nbt.Compound {
	tag := nbt.Copy(cart).(nbt.Compound)
	tag["UUID"] = nbt.UUIDToIntArray(uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "mineshaft_cart/%d/%d/%d/%d", seed, x, y, z)))
	tag["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(float64(x) + 0.5), nbt.Double(float64(y) + 0.5), nbt.Double(float64(z) + 0.5)}}
	tag["Motion"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}}
	return tag
}
