package world

import (
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// terrainSource is what a dimension's generator gives a decorRegion: the
// shaped terrain around a chunk, its decoration state and its biomes.
type terrainSource interface {
	// cachedTerrain returns a chunk's shaped terrain if kept.
	cachedTerrain(cx, cz int32) *shapedTerrain
	// terrainOf returns a chunk's shaped terrain; a chunk being generated
	// passes itself in as shaped so it is not shaped twice.
	terrainOf(cx, cz int32, shaped *Chunk) *shapedTerrain
	decor() *decorState
	isAir(state uint16) bool
	// biomeIndex is the index in the dimension's biome list of the biome a
	// feature's filter reads at a block.
	biomeIndex(r *decorRegion, x, y, z int32) uint
}

// decorRegion is the world a decoration sees: the terrain under every block
// the surrounding protos hold.
type decorRegion struct {
	// src is the dimension's generator; g is the nether's, nil in the
	// overworld, for the features that read its tables.
	src terrainSource
	g   *NetherGenerator
	// minY and height are the dimension's lowest block and block height.
	minY, height int32
	center       *Chunk
	chunks       map[ChunkPos]*shapedTerrain
	// near and protos cover the 5x5 around the centre, row by row from the
	// north-west; a proto is set once a decoration reaches its chunk.
	near   [decorRegionSpan * decorRegionSpan]*shapedTerrain
	protos [decorRegionSpan * decorRegionSpan]*decorProto
	biomes map[ChunkPos]netherQuarts
	// biomeCells reads the overworld's stored biome cells; nil in the nether.
	biomeCells *cellReader
	rng        *levelgen.WorldgenRandom
	// region is WorldGenRegion.random for chunk nx, nz, made on first use.
	region levelgen.RandomSource
	// unorderedSet marks full-hash tree ties requiring JVM identity hashes
	// for iteration order (blockPosSetOrder).
	unorderedSet bool
	// nx, nz is the chunk being decorated; writes outside its 3x3 are dropped.
	// vanilla: WorldGenRegion.ensureCanWrite
	nx, nz int32
	// leafLevels are updateLeaves' toCheck sets, reset between trees.
	leafLevels [7]blockPosSet
	// tested is the ore veins' visited set, reused between veins.
	tested []bool
	// floors caches each near column's OCEAN_FLOOR_WG height minus minY plus
	// one, 0 for unread. Decoration does not update worldgen heightmaps.
	floors []int32
	// worldSeed seeds the noise a geode roughens its shells with, geode caches it.
	worldSeed int64
	geode     *terrain.NoiseStack
}

// decorRegionSpan is the width in chunks a decoration reads densely: the
// decorated chunks plus the ring each writes into.
const decorRegionSpan = 5

// slot is the index of a block's chunk in near and protos, or -1 outside them.
func (r *decorRegion) slot(x, z int32) int {
	dx, dz := x>>4-r.center.x+decorRegionSpan/2, z>>4-r.center.z+decorRegionSpan/2
	if dx < 0 || dz < 0 || dx >= decorRegionSpan || dz >= decorRegionSpan {
		return -1
	}
	return int(dz*decorRegionSpan + dx)
}

func newDecorRegion(src terrainSource, c *Chunk, minY, height int32) *decorRegion {
	r := &decorRegion{
		src:    src,
		minY:   minY,
		height: height,
		center: c,
		chunks: map[ChunkPos]*shapedTerrain{},
		rng:    levelgen.NewWorldgenRandom(0),
	}
	r.prefetch()
	return r
}

// prefetch keeps halo shaping inside the caller's generation permit.
func (r *decorRegion) prefetch() {
	for i := range r.near {
		cx := r.center.x + int32(i%decorRegionSpan) - decorRegionSpan/2
		cz := r.center.z + int32(i/decorRegionSpan) - decorRegionSpan/2
		if cx == r.center.x && cz == r.center.z {
			r.near[i] = r.src.terrainOf(cx, cz, r.center)
			continue
		}
		if t := r.src.cachedTerrain(cx, cz); t != nil {
			r.near[i] = t
			continue
		}
		r.near[i] = r.src.terrainOf(cx, cz, nil)
	}
}

// get returns the block at a world position, air outside the dimension's
// height.
// vanilla: WorldGenRegion.getBlockState
func (r *decorRegion) get(x, y, z int32) uint16 {
	if y < r.minY || y >= r.minY+r.height {
		return airState
	}
	if i := r.slot(x, z); i >= 0 {
		if p := r.protos[i]; p != nil {
			if s, ok := p.get(x, y-r.minY, z); ok {
				return s
			}
		}
	}
	return r.terrainAt(x, y, z)
}

// terrainAt is the block the shaped terrain holds, before any decoration
// wrote; the worldgen heightmaps read it. y must be inside the dimension.
func (r *decorRegion) terrainAt(x, y, z int32) uint16 {
	return r.terrainChunk(x, z).blockAt(x&15, y, z&15)
}

// terrainChunk is the shaped terrain of the chunk holding column (x, z).
func (r *decorRegion) terrainChunk(x, z int32) *shapedTerrain {
	if i := r.slot(x, z); i >= 0 {
		return r.near[i]
	}
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	c, ok := r.chunks[pos]
	if !ok {
		c = r.src.terrainOf(pos.X, pos.Z, nil)
		r.chunks[pos] = c
	}
	return c
}

// BlockAt is get for the survival rules, which read through BlockReader.
func (r *decorRegion) BlockAt(x, y, z int32) uint16 { return r.get(x, y, z) }

// set writes a block unless outside the decorated chunk's 3x3 or the
// dimension's height.
// vanilla: WorldGenRegion.setBlock
func (r *decorRegion) set(x, y, z int32, state uint16) {
	r.setBlock(x, y, z, state, false)
}

// setKnownShape keeps tree and bulk ore writes out of automatic post-processing.
// vanilla: TreeFeature.setBlockKnownShape, OreFeature.doPlace
func (r *decorRegion) setKnownShape(x, y, z int32, state uint16) {
	r.setBlock(x, y, z, state, true)
}

func (r *decorRegion) setBlock(x, y, z int32, state uint16, knownShape bool) {
	if y < r.minY || y >= r.minY+r.height || absInt32(x>>4-r.nx) > 1 || absInt32(z>>4-r.nz) > 1 {
		return
	}
	old := state
	if r.src.decor().lightReplay != nil {
		old = r.get(x, y, z)
	}
	proto := r.protoAt(x, z)
	proto.set(x, y-r.minY, z, state)
	if len(proto.entities) > 0 && !entityBlocks[registry.BlockOf(state)] {
		proto.dropBlockEntity(x&15, y-r.minY, z&15)
	}
	if replay := r.src.decor().lightReplay; replay != nil {
		pos := ChunkPos{X: x >> 4, Z: z >> 4}
		if light, exists := replay[pos]; exists && light.initialized {
			r.src.decor().lightDirty[pos] = true
			if light.sources != nil && !registry.SameLight(old, state) {
				i := (z&15)*16 + x&15
				light.sources[i] = updateSkySourceFloor(r.get, x, y, z, r.minY, light.sources[i])
			}
		}
	}
	if !knownShape {
		switch registry.BlockOf(state) {
		case registry.BlockBrownMushroom, registry.BlockRedMushroom:
			r.markPost(x, y, z)
		case registry.BlockMagmaBlock, registry.BlockSoulSand:
			// vanilla: Blocks.postProcessAbove
			r.markPost(x, y+1, z)
		}
	}
}

// entityBlocks are the blocks that have a block entity.
var entityBlocks = registry.BlocksWhere(func(name string) bool {
	_, ok := registry.BlockEntityTypes[name]
	return ok
})

// protoAt returns a block's chunk proto, creating it empty; the region reads
// only existing protos, so a chunk nothing wrote into keeps none.
func (r *decorRegion) protoAt(x, z int32) *decorProto {
	i := r.slot(x, z)
	if r.protos[i] == nil {
		r.protos[i] = r.src.decor().protos.ensure(ChunkPos{X: x >> 4, Z: z >> 4})
	}
	return r.protos[i]
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (r *decorRegion) isAir(x, y, z int32) bool { return r.src.isAir(r.get(x, y, z)) }

// motionBlockingHeight is the MOTION_BLOCKING read: one above the highest
// motion-blocking block, 0 for an empty column.
// vanilla: WorldGenRegion.getHeight
func (r *decorRegion) motionBlockingHeight(x, z int32) int32 {
	return columnMotionBlocking(r.get, x, z, r.minY, r.minY+r.height-1, 0)
}

// columnMotionBlocking scans a column down from top to bottom for the highest
// motion-blocking block and returns one above it, or empty when there is none.
func columnMotionBlocking(get func(x, y, z int32) uint16, x, z, bottom, top, empty int32) int32 {
	for y := top; y >= bottom; y-- {
		if motionBlockingByState[get(x, y, z)] {
			return y + 1
		}
	}
	return empty
}

// heightAt is the height a heightmap holds: one above the highest block match
// accepts, the dimension's lowest block for an empty column.
// vanilla: WorldGenRegion.getHeight
func (r *decorRegion) heightAt(match func(uint16) bool, x, z int32) int32 {
	return r.scanDown(x, z, match)
}

// scanDown returns one above the highest block of column (x, z) hit accepts,
// r.minY when there is none. A terrain section with no proto blocks over it
// is uniform and costs one hit test.
func (r *decorRegion) scanDown(x, z int32, hit func(uint16) bool) int32 {
	var p *decorProto
	if i := r.slot(x, z); i >= 0 {
		p = r.protos[i]
	}
	t := r.terrainChunk(x, z)
	for y := r.minY + r.height - 1; y >= r.minY; {
		section := (y - worldMinY) / sectionSize
		if t.blocks[section] == nil && (p == nil || p.blocks[(y-r.minY)>>4] == nil) {
			if hit(t.fill[section]) {
				return y + 1
			}
			y = worldMinY + section*sectionSize - 1
			continue
		}
		s, ok := uint16(0), false
		if p != nil {
			s, ok = p.get(x, y-r.minY, z)
		}
		if !ok {
			s = t.blockAt(x&15, y, z&15)
		}
		if hit(s) {
			return y + 1
		}
		y--
	}
	return r.minY
}

// bindProtos makes the existing protos of pos and its eight neighbours
// available to the region, marking them used.
func (r *decorRegion) bindProtos(pos ChunkPos) {
	st := &r.src.decor().protos
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			x, z := (pos.X+dx)*sectionSize, (pos.Z+dz)*sectionSize
			i := r.slot(x, z)
			if r.protos[i] == nil {
				r.protos[i] = st.lookup(ChunkPos{X: pos.X + dx, Z: pos.Z + dz})
			}
			if r.protos[i] != nil {
				st.touch(r.protos[i])
			}
		}
	}
}

// placementMod is a PlacementModifier passing on the positions it makes from
// one, depth first, so each position's whole chain draws before the next.
// vanilla: PlacedFeature.placeWithContext
type placementMod func(r *decorRegion, p [3]int32, next walkNext)

func (r *decorRegion) walk(mods []placementMod, p [3]int32, place func(*decorRegion, [3]int32)) {
	walkNext{r, mods, place}.call(p)
}

// walkNext is the rest of a placement chain, a value so that handing it to a
// modifier allocates nothing.
type walkNext struct {
	r     *decorRegion
	mods  []placementMod
	place func(*decorRegion, [3]int32)
}

func (w walkNext) call(p [3]int32) {
	if len(w.mods) == 0 {
		w.place(w.r, p)
		return
	}
	w.mods[0](w.r, p, walkNext{w.r, w.mods[1:], w.place})
}

func countMod(n int32) placementMod {
	return func(_ *decorRegion, p [3]int32, next walkNext) {
		for range n {
			next.call(p)
		}
	}
}

// countUniformMod is CountPlacement over UniformInt.
func countUniformMod(lo, hi int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		for range r.rng.NextIntN(hi-lo+1) + lo {
			next.call(p)
		}
	}
}

// inSquareMod spreads over the chunk. vanilla: InSquarePlacement
func inSquareMod(r *decorRegion, p [3]int32, next walkNext) {
	x := r.rng.NextIntN(16) + p[0]
	z := r.rng.NextIntN(16) + p[2]
	next.call([3]int32{x, p[1], z})
}

// heightUniformMod is HeightRangePlacement over UniformHeight with the
// anchors already resolved to blocks.
func heightUniformMod(lo, hi int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		next.call([3]int32{p[0], r.rng.NextIntN(hi-lo+1) + lo, p[2]})
	}
}

// heightVeryBiasedMod is HeightRangePlacement over VeryBiasedToBottomHeight
// with the anchors already resolved to blocks. Mth.nextInt draws nothing for
// an empty range.
// vanilla: VeryBiasedToBottomHeight.sample
func heightVeryBiasedMod(lo, hi, inner int32) placementMod {
	nextInt := func(r *decorRegion, a, b int32) int32 {
		if a >= b {
			return a
		}
		return r.rng.NextIntN(b-a+1) + a
	}
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if hi-lo-inner+1 <= 0 {
			next.call([3]int32{p[0], lo, p[2]})
			return
		}
		i := nextInt(r, lo+inner, hi)
		j := nextInt(r, lo, i-1)
		next.call([3]int32{p[0], nextInt(r, lo, j-1+inner), p[2]})
	}
}

// heightTriangleMod is HeightRangePlacement over TrapezoidHeight with no
// plateau.
func heightTriangleMod(lo, hi int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		rg := hi - lo
		start := rg / 2
		y := lo + r.rng.NextIntN(rg-start+1) + r.rng.NextIntN(start+1)
		next.call([3]int32{p[0], y, p[2]})
	}
}

// heightmapMotionBlockingMod is HeightmapPlacement for MOTION_BLOCKING.
func heightmapMotionBlockingMod(r *decorRegion, p [3]int32, next walkNext) {
	if y := r.motionBlockingHeight(p[0], p[2]); y > 0 {
		next.call([3]int32{p[0], y, p[2]})
	}
}

// rarityMod is RarityFilter.
func rarityMod(chance int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if r.rng.NextFloat() < 1.0/float32(chance) {
			next.call(p)
		}
	}
}

// biomeMod is BiomeFilter: the biome at the position, as an index into the
// dimension's biomes, must be in the feature's mask.
func biomeMod(mask uint64) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if mask&(1<<r.src.biomeIndex(r, p[0], p[1], p[2])) != 0 {
			next.call(p)
		}
	}
}

// triangle is TrapezoidInt.sample for min -n, max n and no plateau.
func triangle(rng *levelgen.WorldgenRandom, n int32) int32 {
	a := rng.NextIntN(n + 1)
	return a - rng.NextIntN(n+1)
}

// offsetMod is RandomOffsetPlacement with triangle providers: the x, y and z
// draws in that order. vanilla: TrapezoidInt.sample
func offsetMod(xz, y int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		dx := triangle(r.rng, xz)
		dy := triangle(r.rng, y)
		dz := triangle(r.rng, xz)
		next.call([3]int32{p[0] + dx, p[1] + dy, p[2] + dz})
	}
}

// filterMod is BlockPredicateFilter.
func filterMod(pred func(r *decorRegion, p [3]int32) bool) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if pred(r, p) {
			next.call(p)
		}
	}
}

// placeSpring is SpringFeature: a fluid source in a hole whose four sides and
// floor hold exactly rocks valid blocks and holes open cells.
// vanilla: SpringFeature.place
func (r *decorRegion) placeSpring(fluid uint16, p [3]int32, valid func(uint16) bool, requiresBelow bool, rocks, holes int) {
	x, y, z := p[0], p[1], p[2]
	if !valid(r.get(x, y+1, z)) {
		return
	}
	if requiresBelow && !valid(r.get(x, y-1, z)) {
		return
	}
	if cur := r.get(x, y, z); !r.src.isAir(cur) && !valid(cur) {
		return
	}
	var rockCount, holeCount int
	for _, d := range [5][3]int32{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		s := r.get(x+d[0], y+d[1], z+d[2])
		if valid(s) {
			rockCount++
		}
		if r.src.isAir(s) {
			holeCount++
		}
	}
	if rockCount != rocks || holeCount != holes {
		return
	}
	r.set(x, y, z, fluid)
	r.scheduleTick(x, y, z)
}

// scheduleTick books a tick at (x, y, z) for the chunk's first tick.
func (r *decorRegion) scheduleTick(x, y, z int32) {
	ps := r.protoAt(x, z)
	ps.sources = append(ps.sources, posKey{x & 15, y - r.minY, z & 15})
}

// decorateAround decorates the not-yet-decorated chunks around the centre, x
// outer and z inner, then writes what landed in it with finish. Shaping runs
// alongside other chunks' generation; claims protect overlapping decorations.
// vanilla: ChunkGenerationTask.scheduleLayer
func (r *decorRegion) decorateAround(decorate func(nx, nz int32), finish func(own *decorProto)) {
	r.src.decor().decorateAround(ChunkPos{X: r.center.x, Z: r.center.z}, func(pos ChunkPos) {
		r.decorateChunk(pos, decorate)
	}, finish)
}

// decorateAround shares FEATURES results until a chunk and its neighbours are final.
// vanilla: ChunkGenerationTask.scheduleLayer
func (decor *decorState) decorateAround(pos ChunkPos, decorate func(ChunkPos), finish func(*decorProto)) {
	decor.claim(pos)
	defer decor.release(pos)
	st := &decor.protos
	// A final centre's missing neighbours were dropped once complete.
	centreFinal := st.final(pos)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			next := ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
			if _, ok := st.flags(next); centreFinal && !ok {
				continue
			}
			decorate(next)
		}
	}
	own := st.ensure(pos)
	finish(own)
	st.touch(own)
	if !own.final {
		own.final = true
		st.markFinal(pos)
		decor.cache.markFinal(pos)
	}
	decor.trim()
}

// decorateChunk runs a chunk's features once, leaving its proto unfinished;
// called under a decoration claim.
func (r *decorRegion) decorateChunk(pos ChunkPos, decorate func(nx, nz int32)) {
	if r.src.decor().protos.decorated(pos) {
		return
	}
	r.bindProtos(pos)
	decorate(pos.X, pos.Z)
	r.protoAt(pos.X*sectionSize, pos.Z*sectionSize).decorated = true
}

// regionRandom is the stream features reach through level.getRandom(), apart
// from the feature's own; it is seeded per decorated chunk.
// vanilla: WorldGenRegion.random
func (r *decorRegion) regionRandom() levelgen.RandomSource {
	if r.region == nil {
		r.region = levelgen.NewXoroshiroNamedRandom(r.worldSeed, "minecraft:worldgen_region_random").At(r.nx*16, 0, r.nz*16)
	}
	return r.region
}

// wgHeight reads the terrain's frozen heights: FEATURES never updates the worldgen heightmaps and a saved proto keeps them.
// vanilla: ChunkStatus.FEATURES, SerializableChunkData.copyOf
func (r *decorRegion) wgHeight(kind heightmapKind, x, z int32) int32 {
	t := r.terrainChunk(x, z)
	slot := &t.wg[kind][x&15|(z&15)<<4]
	if v := atomic.LoadInt32(slot); v != 0 {
		return v - 1 + r.minY
	}
	height := r.minY
	for y := r.minY + r.height - 1; y >= r.minY; {
		// Uniform sections need one predicate check, including empty sky.
		section := (y - worldMinY) / sectionSize
		s := t.fill[section]
		uniform := t.blocks[section] == nil
		if !uniform {
			s = t.blockAt(x&15, y, z&15)
		}
		if (kind == hmWorldSurfaceWG && !r.src.isAir(s)) || (kind == hmOceanFloorWG && oceanFloorByState[s]) {
			height = y + 1
			break
		}
		if uniform {
			y = worldMinY + section*sectionSize - 1
		} else {
			y--
		}
	}
	atomic.StoreInt32(slot, height-r.minY+1)
	return height
}

// featuresSkyLight reads the zero-filled initialization halo, before LIGHT
// propagates. Callers hold a decoration claim, as for get and bindProtos.
// vanilla: LayerLightSectionStorage.updateSectionStatus, SkyLightSectionStorage.getLightValue
func (r *decorRegion) featuresSkyLight(x, y, z int32) int {
	st := &r.src.decor().protos
	for cx := (x >> 4) - 1; cx <= (x>>4)+1; cx++ {
		for cz := (z >> 4) - 1; cz <= (z>>4)+1; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			var p *decorProto
			if i := r.slot(cx*16, cz*16); i >= 0 {
				p = r.protos[i]
			}
			if p == nil {
				p = st.lookup(pos)
			}
			if p == nil || !p.decorated {
				continue
			}
			if replay := r.src.decor().lightReplay; replay != nil {
				state, initialized := replay[pos]
				if initialized && state.initialized && state.occupied && y>>4 <= state.top+1 {
					return 0
				}
				continue
			}
			// Contributors use their own terrain, including proto writes not bound
			// to this region. Occupancy can change after any neighbour's write.
			var t *shapedTerrain
			if i := r.slot(cx*16, cz*16); i >= 0 {
				t = r.near[i]
			} else {
				t = r.chunks[pos]
			}
			if t == nil {
				t = r.src.cachedTerrain(cx, cz)
			}
			if t == nil {
				continue
			}
			top, occupied := decorTopSection(p, t, r.minY, r.height)
			if occupied && y>>4 <= top+1 {
				return 0
			}
		}
	}
	return 15
}

func decorTopSection(p *decorProto, t *shapedTerrain, minY, height int32) (int32, bool) {
	for sy := minY + height - 16; sy >= minY; sy -= 16 {
		si := (sy - minY) / 16
		ti := (sy - worldMinY) / 16
		occupied := false
		if p.blocks[si] == nil && t.blocks[ti] == nil {
			occupied = !IsKnownAirState(t.fill[ti])
		} else {
			for dy := int32(0); dy < 16 && !occupied; dy++ {
				for z := int32(0); z < 16 && !occupied; z++ {
					for x := range int32(16) {
						state, ok := p.get(x, sy+dy-minY, z)
						if !ok {
							state = t.blockAt(x, sy+dy, z)
						}
						if !IsKnownAirState(state) {
							occupied = true
							break
						}
					}
				}
			}
		}
		if occupied {
			return sy >> 4, true
		}
	}
	return 0, false
}
