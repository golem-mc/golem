package world

//go:generate go run ../../../tools/gen nether-features

import (
	"container/list"
	_ "embed"
	"math"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// The nether's placed features, run as ChunkGenerator.applyBiomeDecoration
// does. A feature may write into neighbouring chunks, so each chunk is
// decorated once over a decorRegion (the terrain and protos in reach), and
// writes landing in a neighbour stay in its proto (decor_protos.go). Overlaps
// depend on chunk request order, in vanilla too.

// netherGenHeight is the nether dimension's height, bounding feature writes;
// the terrain fills its lower netherTop blocks.
// vanilla: DimensionType the_nether
const netherGenHeight = 256

// netherMaxGenY is VerticalAnchor.belowTop(0), the dimension cut to the noise
// settings' 128 blocks.
// vanilla: WorldGenerationContext.getMaxGenY
const netherMaxGenY = netherTop - 1

// shapedTerrainCap bounds the shaped chunks kept per generator.
// Interleaved exploration needs room for several simultaneous decoration halos.
const shapedTerrainCap = 4096

// shapedTerrain is a chunk's carved terrain, shared read-only between
// decorations.
type shapedTerrain struct {
	fluidUpdates []posKey
	// fill and blocks are each section's, as section holds them.
	fill   [sectionCount]uint16
	blocks [sectionCount]*blockWords
	// wg caches the WORLD_SURFACE_WG and OCEAN_FLOOR_WG heights per column
	// (x&15 | (z&15)<<4) as height-minY+1, 0 meaning not computed. Regions
	// reading one frozen chunk share it through atomic loads and stores of
	// the same value; not atomic.Int32, as tests copy shapedTerrain.
	wg [2][256]int32
}

func (t *shapedTerrain) blockAt(x, y, z int32) uint16 {
	if !contains(y) {
		return airState
	}
	y -= worldMinY
	if b := t.blocks[y/sectionSize]; b != nil {
		return b.get(blockIndex(x, y%sectionSize, z))
	}
	return t.fill[y/sectionSize]
}

// keep takes c's blocks, copying the arrays when clone, for a chunk that goes
// on to be decorated.
func (t *shapedTerrain) keep(c *Chunk, clone bool) {
	t.fluidUpdates = append([]posKey(nil), c.pendingGenerated...)
	for i := range c.sections {
		t.fill[i] = c.sections[i].fill
		b := c.sections[i].blocks.Load()
		if clone && b != nil {
			b = b.clone()
		}
		t.blocks[i] = b
	}
}

// restore gives c a copy of the terrain's blocks, as keep took them.
func (t *shapedTerrain) restore(c *Chunk) {
	c.pendingGenerated = append(c.pendingGenerated[:0], t.fluidUpdates...)
	for i := range c.sections {
		s := &c.sections[i]
		s.fill = t.fill[i]
		var b *blockWords
		if t.blocks[i] != nil {
			b = t.blocks[i].clone()
		}
		s.spawn.Store(nil)
		s.blocks.Store(b)
		s.recalcCounts()
	}
}

type terrainEntry struct {
	pos    ChunkPos
	t      *shapedTerrain // set once ready is closed
	ready  chan struct{}
	finals uint32 // bit (dz+2)*5+(dx+2) for final chunks within distance 2
	elem   *list.Element
}

// shapedTerrainCache keeps recently shaped chunks, since each decoration reads
// the 5x5 around it.
type shapedTerrainCache struct {
	mu    sync.Mutex
	byPos map[ChunkPos]*terrainEntry
	lru   list.List // front = most recently used
	cap   int       // 0 means shapedTerrainCap
	// compressing holds entries evict removed from byPos that spill has not
	// filed in cold yet; they stay readable.
	compressing map[ChunkPos]*terrainEntry
	cold        map[ChunkPos]*coldTerrain
	coldLRU     list.List // front = most recently used
	coldBytes   int
}

// takeBack returns an entry still compressing to the hot tier. Callers hold c.mu.
func (c *shapedTerrainCache) takeBack(pos ChunkPos) *terrainEntry {
	e := c.compressing[pos]
	if e != nil {
		delete(c.compressing, pos)
		e.elem = c.lru.PushFront(e)
		c.byPos[pos] = e
	}
	return e
}

// cached returns ready terrain without waiting for an in-flight shape.
func (c *shapedTerrainCache) cached(pos ChunkPos) *shapedTerrain {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byPos[pos]
	if e == nil && c.compressing[pos] != nil {
		e = c.takeBack(pos)
	}
	if e != nil {
		select {
		case <-e.ready:
			c.lru.MoveToFront(e.elem)
			return e.t
		default:
		}
	}
	return nil
}

// of shares a single shape among callers; shaped belongs to a caller that will decorate it.
func (c *shapedTerrainCache) of(pos ChunkPos, shaped *Chunk, limit chan struct{}, shape func(*Chunk)) *shapedTerrain {
	c.mu.Lock()
	if c.byPos == nil {
		c.byPos = map[ChunkPos]*terrainEntry{}
	}
	e := c.byPos[pos]
	if e == nil {
		e = c.takeBack(pos)
	}
	if e != nil {
		c.lru.MoveToFront(e.elem)
		c.mu.Unlock()
		<-e.ready
		return e.t
	}
	e = &terrainEntry{pos: pos, ready: make(chan struct{})}
	e.elem = c.lru.PushFront(e)
	c.byPos[pos] = e
	cold := c.cold[pos]
	if cold != nil {
		e.finals = cold.finals
		c.removeCold(cold)
	}
	c.mu.Unlock()
	var t *shapedTerrain
	if cold != nil && shaped == nil {
		t = inflateTerrain(cold)
	}
	switch {
	case t != nil:
	case shaped != nil:
		t = &shapedTerrain{}
		t.keep(shaped, true)
	default:
		t = &shapedTerrain{}
		if limit != nil {
			limit <- struct{}{}
		}
		chunk := newChunk(pos.X, pos.Z)
		shape(chunk)
		t.keep(chunk, false) // only throwaway chunks may donate their arrays
		if limit != nil {
			<-limit
		}
	}
	c.mu.Lock()
	e.t = t
	close(e.ready)
	victims := c.evict()
	c.mu.Unlock()
	c.spill(victims)
	return t
}

func (c *shapedTerrainCache) remove(e *terrainEntry) {
	delete(c.byPos, e.pos)
	c.lru.Remove(e.elem)
}

// evict moves ready entries past the cap to compressing and returns them for spill.
func (c *shapedTerrainCache) evict() (victims []*terrainEntry) {
	limit := c.cap
	if limit == 0 {
		limit = shapedTerrainCap
	}
	for elem := c.lru.Back(); elem != nil && len(c.byPos) > limit; {
		e := elem.Value.(*terrainEntry)
		elem = elem.Prev()
		select {
		case <-e.ready:
			c.remove(e)
			if c.compressing == nil {
				c.compressing = map[ChunkPos]*terrainEntry{}
			}
			c.compressing[e.pos] = e
			victims = append(victims, e)
		default:
			// In-flight shapes keep their single-flight entry until publication.
		}
	}
	return victims
}

func (c *shapedTerrainCache) markFinal(pos ChunkPos) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			p := ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
			bit := uint32(1) << ((2-dz)*5 + 2 - dx)
			if e := c.byPos[p]; e != nil {
				e.finals |= bit
				if e.finals == 1<<25-1 {
					c.remove(e)
				}
			} else if e := c.compressing[p]; e != nil {
				e.finals |= bit
				if e.finals == 1<<25-1 {
					delete(c.compressing, p)
				}
			} else if e := c.cold[p]; e != nil {
				e.finals |= bit
				if e.finals == 1<<25-1 {
					c.removeCold(e)
				}
			}
		}
	}
}

func (g *NetherGenerator) cachedTerrain(cx, cz int32) *shapedTerrain {
	return g.cache.cached(ChunkPos{X: cx, Z: cz})
}

func (g *NetherGenerator) terrainOf(cx, cz int32, shaped *Chunk) *shapedTerrain {
	return g.cache.of(ChunkPos{X: cx, Z: cz}, shaped, g.shapers, func(c *Chunk) {
		g.buildTerrain(c, true)
	})
}

func newNetherRegion(g *NetherGenerator, c *Chunk) *decorRegion {
	r := newDecorRegion(g, c, 0, netherGenHeight)
	r.g = g
	r.biomes = map[ChunkPos]netherQuarts{}
	return r
}

func (g *NetherGenerator) decor() *decorState { return &g.decorState }

func (g *NetherGenerator) biomeIndex(r *decorRegion, x, y, z int32) uint {
	return uint(r.biomeAt(x, y, z))
}

// biomeAt is the biome a feature's filter reads at a block: the zoomed quart's
// noise biome.
// vanilla: BiomeManager.getBiome
func (r *decorRegion) biomeAt(x, y, z int32) levelgen.NetherBiome {
	qx, _, qz := levelgen.ZoomedQuart(r.g.zoomSeed, x, y, z)
	return r.quartBiome(qx, qz)
}

func (r *decorRegion) quartBiome(qx, qz int32) levelgen.NetherBiome {
	key := ChunkPos{X: qx >> 2, Z: qz >> 2}
	q, ok := r.biomes[key]
	if !ok {
		// Read the owning chunk's biome fill: a fresh climate search can
		// choose a different biome when RTree fitness ties.
		// vanilla: LevelReader.getNoiseBiome, Climate.RTree.lastResult
		q = r.g.quartBiomes(key.X, key.Z)
		r.biomes[key] = q
	}
	return q[(qx&3)+1][(qz&3)+1]
}

// decorateNether decorates the not-yet-decorated chunks around c, then writes
// what landed in c.
func (g *NetherGenerator) decorateNether(c *Chunk) {
	r := newNetherRegion(g, c)
	r.decorateAround(r.decorate, r.finish)
}

// DecorateInOrder runs each chunk's features once in seq order, without
// finishing chunks, so Generate can consume a vanilla recording.
// vanilla: ChunkStatusTasks.generateFeatures
func (g *NetherGenerator) DecorateInOrder(seq []ChunkPos) {
	for _, pos := range seq {
		c := newChunk(pos.X, pos.Z)
		g.buildTerrain(c, true)
		r := newNetherRegion(g, c)
		g.claimAll()
		r.decorateChunk(pos, r.decorate)
		g.releaseAll()
	}
}

// finish updates the shapes of the chunk's marked blocks, now that its 3x3 is
// decorated, and hands its springs and block entities to the chunk.
func (r *decorRegion) finish(own *decorProto) {
	c := r.center
	r.reconnectShapes(own)
	var dummies []dummyEntity
	for y := range int32(netherGenHeight) {
		if own.blocks[y>>4] == nil {
			continue
		}
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				if s, ok := own.get(x, y, z); ok {
					c.SetBlock(x, y, z, s)
					dummies = appendDummy(dummies, c.x*sectionSize+x, y, c.z*sectionSize+z, s)
				}
			}
		}
	}
	for _, k := range own.sources {
		c.scheduleGenerated(k[0], k[1], k[2])
	}
	own.transferFluidTicks(c, 0)
	for _, be := range own.entities {
		be.place(c, 0)
	}
	c.promoteDummies(dummies)
}

// decorate runs chunk (nx, nz)'s features in step and index order. A feature
// runs when a biome carrying it is among the surrounding chunks; each placement
// is then filtered on its own position's biome.
// vanilla: ChunkGenerator.applyBiomeDecoration
func (r *decorRegion) decorate(nx, nz int32) {
	r.nx, r.nz = nx, nz
	var possible uint8
	for qx := (nx - 1) * 4; qx < (nx+2)*4; qx++ {
		for qz := (nz - 1) * 4; qz < (nz+2)*4; qz++ {
			possible |= 1 << r.quartBiome(qx, qz)
		}
	}
	ox, oz := nx*sectionSize, nz*sectionSize
	seed := r.rng.SetDecorationSeed(r.g.seed, ox, oz)
	// Each step places its structures before its features.
	bastionDone, fortressDone := false, false
	for i := range r.g.placed {
		f := &r.g.placed[i]
		if !bastionDone && f.feature.Slot.Step >= bastionStep {
			bastionDone = true
			r.placeSurfaceStructures(seed, nx, nz)
		}
		if !fortressDone && f.feature.Slot.Step >= fortressWritingStep {
			fortressDone = true
			r.placeFortress(seed, nx, nz)
			r.placeFossils(nx, nz)
		}
		if f.feature.Biomes&possible == 0 {
			continue
		}
		r.rng.SetFeatureSeed(seed, f.feature.Slot.Index, f.feature.Slot.Step)
		r.walk(f.mods, [3]int32{ox, 0, oz}, f.place)
	}
	if !bastionDone {
		r.placeSurfaceStructures(seed, nx, nz)
	}
	if !fortressDone {
		r.placeFortress(seed, nx, nz)
		r.placeFossils(nx, nz)
	}
}

// netherPlaced is a placed feature: its placement modifiers and the feature
// they feed.
type netherPlaced struct {
	feature levelgen.NetherFeature
	mods    []placementMod
	place   func(r *decorRegion, p [3]int32)
}

// initNetherFeatures builds the placed features in levelgen.NetherFeatures
// order. spring_lava places nothing: its valid blocks are the overworld's stone
// and dirt, none of which the nether has.
func (g *NetherGenerator) initNetherFeatures() {
	var (
		lo4, hi4   = int32(4), int32(netherMaxGenY - 4)
		lo10, hi10 = int32(10), int32(netherMaxGenY - 10)
	)
	air := func(r *decorRegion, p [3]int32) bool { return r.isAir(p[0], p[1], p[2]) }
	patchOffset := offsetMod(7, 3)
	airFilter := filterMod(air)
	ground := func(state uint16) placementMod {
		return filterMod(func(r *decorRegion, p [3]int32) bool {
			return air(r, p) && r.get(p[0], p[1]-1, p[2]) == state
		})
	}
	netherrackOnly := func(s uint16) bool { return s == g.netherrackState }
	deltaSprings := map[uint16]bool{
		g.netherrackState: true, g.soulSandState: true, g.gravelState: true,
		g.magmaState: true, g.blackstoneState: true,
	}
	deltaSpring := func(s uint16) bool { return deltaSprings[s] }
	baseStone := func(s uint16) bool {
		return s == g.netherrackState || s == g.basaltState || s == g.blackstoneState
	}
	// vanilla: FireBlock.canSurvive, MushroomBlock.canSurvive
	setIf := func(state uint16, survives func(r *decorRegion, p [3]int32) bool) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) {
			if survives(r, p) {
				r.set(p[0], p[1], p[2], state)
			}
		}
	}
	fireSurvives := func(r *decorRegion, p [3]int32) bool {
		return registry.FaceSturdy(r.get(p[0], p[1]-1, p[2]), dirUp, registry.SupportFull)
	}
	mushroomSurvives := func(r *decorRegion, p [3]int32) bool {
		return placementSurvives(r, g.brownMushroomState, p[0], p[1], p[2])
	}
	ore := func(state uint16, size int32) func(*decorRegion, [3]int32) {
		cfg := &oreConfig{size: size, targets: []oreTarget{{func(s uint16, _ int32) bool { return netherrackOnly(s) }, state}}}
		return func(r *decorRegion, p [3]int32) { r.placeOre(p, cfg) }
	}
	scattered := func(size int32) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) { g.placeScatteredOre(r, p, size, baseStone) }
	}
	blobs := func(state uint16) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) { g.placeReplaceBlobs(r, p, state) }
	}
	spring := func(valid func(uint16) bool, requiresBelow bool, rocks, holes int) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) { r.placeSpring(g.lavaState, p, valid, requiresBelow, rocks, holes) }
	}
	// patch_crimson_roots has no in_square, so its positions start at the
	// chunk's corner.
	crimsonRoots := func(b placementMod) ([]placementMod, func(*decorRegion, [3]int32)) {
		mods := []placementMod{heightUniformMod(0, netherMaxGenY), b, countMod(96), patchOffset, airFilter}
		return mods, func(r *decorRegion, p [3]int32) {
			if g.netherPlantSupport(r.get(p[0], p[1]-1, p[2])) {
				r.set(p[0], p[1], p[2], g.crimsonRootsState)
			}
		}
	}

	// A def builds a feature's modifiers around the biome filter, which follows
	// the height in every list.
	type def func(biome placementMod) ([]placementMod, func(*decorRegion, [3]int32))
	band := func(count placementMod, lo, hi int32, place func(*decorRegion, [3]int32)) def {
		return func(b placementMod) ([]placementMod, func(*decorRegion, [3]int32)) {
			return []placementMod{count, inSquareMod, heightUniformMod(lo, hi), b}, place
		}
	}
	patch := func(count, height placementMod, filter placementMod, place func(*decorRegion, [3]int32)) def {
		return func(b placementMod) ([]placementMod, func(*decorRegion, [3]int32)) {
			head := []placementMod{inSquareMod, height, b, countMod(96), patchOffset, filter}
			if count != nil {
				head = append([]placementMod{count}, head...)
			}
			return head, place
		}
	}
	single := func(height placementMod, place func(*decorRegion, [3]int32)) def {
		return func(b placementMod) ([]placementMod, func(*decorRegion, [3]int32)) {
			return []placementMod{inSquareMod, height, b}, place
		}
	}
	layered := func(count int32, place func(*decorRegion, [3]int32)) def {
		return func(b placementMod) ([]placementMod, func(*decorRegion, [3]int32)) {
			return []placementMod{countOnEveryLayerMod(count), b}, place
		}
	}
	fireCount := countUniformMod(0, 5)
	fireHeight := heightUniformMod(lo4, hi4)
	soulBase := func(r *decorRegion, p [3]int32) bool {
		s := r.get(p[0], p[1]-1, p[2])
		return s == g.soulSoilState || s == g.soulSandState
	}
	defs := map[string]def{
		"minecraft:spring_open":          band(countMod(8), lo4, hi4, spring(netherrackOnly, false, 4, 1)),
		"minecraft:spring_closed":        band(countMod(16), lo10, hi10, spring(netherrackOnly, false, 5, 0)),
		"minecraft:spring_closed_double": band(countMod(32), lo10, hi10, spring(netherrackOnly, false, 5, 0)),
		"minecraft:spring_delta":         band(countMod(16), lo4, hi4, spring(deltaSpring, true, 4, 1)),

		"minecraft:patch_fire":      patch(fireCount, fireHeight, ground(g.netherrackState), setIf(g.fireState, fireSurvives)),
		"minecraft:patch_soul_fire": patch(fireCount, fireHeight, ground(g.soulSoilState), setIf(g.soulFireState, soulBase)),

		"minecraft:ore_magma":         band(countMod(4), 27, 36, ore(g.magmaState, 33)),
		"minecraft:ore_soul_sand":     band(countMod(12), 0, 31, ore(g.soulSandState, 12)),
		"minecraft:ore_gravel_nether": band(countMod(2), 5, 41, ore(g.gravelState, 33)),
		"minecraft:ore_blackstone":    band(countMod(2), 5, 31, ore(g.blackstoneState, 33)),
		"minecraft:ore_gold_nether":   band(countMod(10), lo10, hi10, ore(g.goldOreState, 10)),
		"minecraft:ore_quartz_nether": band(countMod(16), lo10, hi10, ore(g.quartzOreState, 14)),
		"minecraft:ore_gold_deltas":   band(countMod(20), lo10, hi10, ore(g.goldOreState, 10)),
		"minecraft:ore_quartz_deltas": band(countMod(32), lo10, hi10, ore(g.quartzOreState, 14)),
		"minecraft:basalt_blobs":      band(countMod(75), 0, netherMaxGenY, blobs(g.basaltState)),
		"minecraft:blackstone_blobs":  band(countMod(25), 0, netherMaxGenY, blobs(g.blackstoneState)),

		"minecraft:delta": layered(40, func(r *decorRegion, p [3]int32) { g.placeDelta(r, p) }),
		"minecraft:small_basalt_columns": layered(4, func(r *decorRegion, p [3]int32) {
			g.placeBasaltColumns(r, p, 1, 4, 1, 1)
		}),
		"minecraft:large_basalt_columns": layered(2, func(r *decorRegion, p [3]int32) {
			g.placeBasaltColumns(r, p, 5, 10, 2, 3)
		}),

		"minecraft:patch_crimson_roots": crimsonRoots,
		"minecraft:crimson_fungi": layered(8, func(r *decorRegion, p [3]int32) {
			placeHugeFungus(r, p, g.fungusConfig(g.crimsonNyliumState, g.crimsonStemState, g.netherWartState, true))
		}),
		"minecraft:warped_fungi": layered(8, func(r *decorRegion, p [3]int32) {
			placeHugeFungus(r, p, g.fungusConfig(g.warpedNyliumState, g.warpedStemState, g.warpedWartState, false))
		}),

		"minecraft:ore_debris_small":         single(heightUniformMod(8, netherMaxGenY-8), scattered(2)),
		"minecraft:ore_ancient_debris_large": single(heightTriangleMod(8, 24), scattered(3)),

		// The nether's own mushrooms take half the chunks at any height; the
		// forests' take 1 in 256 and 1 in 512 from the heightmap.
		"minecraft:brown_mushroom_nether": patch(rarityMod(2), heightUniformMod(0, netherMaxGenY), airFilter, setIf(g.brownMushroomState, mushroomSurvives)),
		"minecraft:red_mushroom_nether":   patch(rarityMod(2), heightUniformMod(0, netherMaxGenY), airFilter, setIf(g.redMushroomState, mushroomSurvives)),
		"minecraft:brown_mushroom_normal": patch(rarityMod(256), heightmapMotionBlockingMod, airFilter, setIf(g.brownMushroomState, mushroomSurvives)),
		"minecraft:red_mushroom_normal":   patch(rarityMod(512), heightmapMotionBlockingMod, airFilter, setIf(g.redMushroomState, mushroomSurvives)),
	}
	jsonFeatures := netherJSONFeatures()
	for _, f := range levelgen.NetherFeatures {
		if doc, ok := jsonFeatures[f.Name]; ok {
			mods, place := compilePlaced(doc, uint64(f.Biomes), true)
			g.placed = append(g.placed, netherPlaced{feature: f, mods: mods, place: func(r *decorRegion, p [3]int32) { place(r, p) }})
			continue
		}
		if d, ok := defs[f.Name]; ok {
			mods, place := d(biomeMod(uint64(f.Biomes)))
			g.placed = append(g.placed, netherPlaced{feature: f, mods: mods, place: place})
		}
	}
}

// placeScatteredOre is ScatteredOreFeature with discard_chance_on_air_exposure
// 1: up to size+1 tries around the origin, each placing only on base stone
// touching no air.
// vanilla: ScatteredOreFeature.place
func (g *NetherGenerator) placeScatteredOre(r *decorRegion, p [3]int32, size int32, target func(uint16) bool) {
	rng := r.rng
	tries := rng.NextIntN(size + 1)
	offset := func(reach int32) int32 {
		return int32(math.Floor(float64((rng.NextFloat()-rng.NextFloat())*float32(reach)) + 0.5))
	}
	for i := range tries {
		reach := min(i, 7)
		x, y, z := p[0]+offset(reach), p[1]+offset(reach), p[2]+offset(reach)
		if !target(r.get(x, y, z)) {
			continue
		}
		touchesAir := false
		for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if r.isAir(x+d[0], y+d[1], z+d[2]) {
				touchesAir = true
				break
			}
		}
		if !touchesAir {
			r.set(x, y, z, g.ancientDebrisState)
		}
	}
}

// placeReplaceBlobs replaces every netherrack within a Manhattan ball around
// the first netherrack below the origin.
// vanilla: ReplaceBlobsFeature.place
func (g *NetherGenerator) placeReplaceBlobs(r *decorRegion, p [3]int32, state uint16) {
	x, z := p[0], p[2]
	y := min(max(p[1], 1), netherGenHeight-1)
	for ; y > 1; y-- {
		if r.get(x, y, z) == g.netherrackState {
			break
		}
	}
	if y <= 1 {
		return
	}
	rx := r.rng.NextIntN(5) + 3
	ry := r.rng.NextIntN(5) + 3
	rz := r.rng.NextIntN(5) + 3
	limit := max(rx, ry, rz)
	for dx := -rx; dx <= rx; dx++ {
		for dy := -ry; dy <= ry; dy++ {
			for dz := -rz; dz <= rz; dz++ {
				if absInt32(dx)+absInt32(dy)+absInt32(dz) > limit {
					continue
				}
				if r.get(x+dx, y+dy, z+dz) == g.netherrackState {
					r.set(x+dx, y+dy, z+dz, state)
				}
			}
		}
	}
}

//go:embed nether_features.txt
var netherFeatureData string

func netherJSONFeatures() map[string]jobj {
	out := map[string]jobj{}
	for line := range strings.SplitSeq(netherFeatureData, "\n") {
		if name, doc, ok := strings.Cut(line, "\t"); ok {
			out["minecraft:"+name] = parseJSON(doc)
		}
	}
	return out
}
