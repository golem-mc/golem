package world

import (
	"math"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// End decorations share proto chunks so later trees see earlier placements,
// including trees reaching across a chunk boundary.

// The End's minimum y and height. vanilla: DimensionType the_end
const (
	endMinY   = 0
	endHeight = 256
)

// The End's features' decoration slots. vanilla: GenerationStep.Decoration
var (
	slotEndIsland        = levelgen.EndFeatureSlots["minecraft:end_island_decorated"]
	slotEndGatewayReturn = levelgen.EndFeatureSlots["minecraft:end_gateway_return"]
	slotEndSpike         = levelgen.EndFeatureSlots["minecraft:end_spike"]
	slotChorusPlant      = levelgen.EndFeatureSlots["minecraft:chorus_plant"]
)

// endChorusSpread is how far a chorus plant's branches reach from its root.
// vanilla: ChorusPlantFeature.place
const endChorusSpread = 8

// endTerrain is one chunk's terrain, one bit per block of the end's 256 layers
// set for end stone, bit index y<<8 | z<<4 | x.
type endTerrain [sectionSize * endHeight * sectionSize / 64]uint64

func (t *endTerrain) solid(x, y, z int32) bool {
	i := y<<8 | z<<4 | x
	return t[i>>6]&(1<<(i&63)) != 0
}

// endTerrainCacheSize bounds the terrain kept for neighbours' decoration, 8 KiB
// a chunk.
const endTerrainCacheSize = 512

// endTerrainCache keeps recent chunk terrain, since each decoration reads its
// neighbours' too.
type endTerrainCache struct {
	mu    sync.Mutex
	byPos map[ChunkPos]*endTerrain
	order []ChunkPos
}

// terrainOf returns the terrain of a chunk, built once and shared read-only.
func (g *EndGenerator) terrainOf(cx, cz int32) *endTerrain {
	pos := ChunkPos{X: cx, Z: cz}
	g.cache.mu.Lock()
	t, ok := g.cache.byPos[pos]
	g.cache.mu.Unlock()
	if ok {
		return t
	}
	t = new(endTerrain)
	nc := terrain.NewNoiseChunk(g.state, nil, g.state.ChunkVolume(cx, cz))
	// The End has no aquifer, so a block is solid exactly where the density is
	// above 0, which is what DoFill would decide.
	density := nc.SampleDensity()
	v := nc.Volume()
	for z := range v.SizeZ {
		for x := range v.SizeX {
			for y := range v.SizeY {
				if by := v.BlockY(y); density[v.Index(x, y, z)] > 0 && by >= endMinY && by < endMinY+endHeight {
					i := (by-endMinY)<<8 | z<<4 | x
					t[i>>6] |= 1 << (i & 63)
				}
			}
		}
	}
	nc.ReleaseDensity(density)
	nc.Recycle()
	g.cache.mu.Lock()
	defer g.cache.mu.Unlock()
	if g.cache.byPos == nil {
		g.cache.byPos = map[ChunkPos]*endTerrain{}
	}
	if _, ok := g.cache.byPos[pos]; !ok {
		if len(g.cache.order) >= endTerrainCacheSize {
			delete(g.cache.byPos, g.cache.order[0])
			g.cache.order = g.cache.order[1:]
		}
		g.cache.byPos[pos] = t
		g.cache.order = append(g.cache.order, pos)
	}
	return t
}

// endRegion is the world a decoration sees: the terrain under every block
// written so far, writes kept by world position.
type endRegion struct {
	g             *EndGenerator
	terrain       map[ChunkPos]*endTerrain
	placed        map[posKey]uint16
	protos        map[ChunkPos]*decorProto
	entities      map[posKey]nbt.Compound
	blockEntities []nbt.Compound
	rng           *levelgen.WorldgenRandom
}

func newEndRegion(g *EndGenerator) *endRegion {
	return &endRegion{
		g:       g,
		terrain: map[ChunkPos]*endTerrain{},
		placed:  map[posKey]uint16{},
		rng:     levelgen.NewWorldgenRandom(0),
	}
}

// get returns the block at a world position, air outside the end's height
// (ProtoChunk.getBlockState's void air).
func (r *endRegion) get(x, y, z int32) uint16 {
	if y < endMinY || y >= endMinY+endHeight {
		return airState
	}
	if s, ok := r.placed[posKey{x, y, z}]; ok {
		return s
	}
	if r.protos != nil {
		if p := r.proto(x, z, false); p != nil {
			if s, ok := p.get(x, y-endMinY, z); ok {
				return s
			}
		}
	}
	cx, cz := x>>4, z>>4
	pos := ChunkPos{X: cx, Z: cz}
	t, ok := r.terrain[pos]
	if !ok {
		t = r.g.terrainOf(cx, cz)
		r.terrain[pos] = t
	}
	if t.solid(x&15, y-endMinY, z&15) {
		return r.g.endStoneState
	}
	return airState
}

// set writes a block. No end feature reaches past its neighbours or out of the
// end's height, so WorldGenRegion.ensureCanWrite never drops one.
// vanilla: WorldGenRegion.setBlock
func (r *endRegion) set(x, y, z int32, state uint16) {
	if r.get(x, y, z) == state {
		return
	}
	if r.protos == nil {
		r.placed[posKey{x, y, z}] = state
	} else {
		p := r.proto(x, z, true)
		p.set(x, y-endMinY, z, state)
		if len(p.entities) > 0 {
			p.dropBlockEntity(x&15, y-endMinY, z&15)
		}
	}
	delete(r.entities, posKey{x, y, z})
}

// proto reads the shared decoration under the region's claim.
func (r *endRegion) proto(x, z int32, create bool) *decorProto {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	p, ok := r.protos[pos]
	if !ok {
		p = r.g.protos.lookup(pos)
		r.protos[pos] = p
	}
	if p == nil && create {
		p = r.g.protos.ensure(pos)
		r.protos[pos] = p
	}
	return p
}

func (r *endRegion) decorateChunk(pos ChunkPos) {
	if r.g.protos.decorated(pos) {
		return
	}
	clear(r.protos)
	r.g.decorate(r, r.g.biomeGrid(pos.X, pos.Z), pos.X, pos.Z)
	for p, tag := range r.entities {
		proto := r.proto(p[0], p[2], true)
		proto.entities = append(proto.entities, protoEntity{x: p[0] & 15, y: p[1] - endMinY, z: p[2] & 15, tag: tag})
	}
	for _, tag := range r.blockEntities {
		x, y, z := carriedPos(tag)
		proto := r.proto(x, z, true)
		proto.entities = append(proto.entities, protoEntity{x: x & 15, y: y - endMinY, z: z & 15, tag: tag})
	}
	clear(r.entities)
	r.blockEntities = r.blockEntities[:0]
	r.proto(pos.X*16, pos.Z*16, true).decorated = true
}

// DecorateInOrder runs FEATURES once per chunk in a vanilla recording's order.
func (g *EndGenerator) DecorateInOrder(seq []ChunkPos) {
	g.claimAll()
	defer g.releaseAll()
	for _, pos := range seq {
		r := newEndRegion(g)
		r.protos = map[ChunkPos]*decorProto{}
		r.decorateChunk(pos)
	}
}

func (r *endRegion) isAir(x, y, z int32) bool {
	return r.get(x, y, z) == airState
}

// motionBlockingHeight is the MOTION_BLOCKING read: one above the highest
// motion-blocking block, or the end's floor for an empty column.
// vanilla: WorldGenRegion.getHeight
func (r *endRegion) motionBlockingHeight(x, z int32) int32 {
	return columnMotionBlocking(r.get, x, z, endMinY, endMinY+endHeight-1, endMinY)
}

// featureBiome is the biome a feature's filter reads at a block: the zoomed
// quart's noise biome.
// vanilla: BiomeManager.getBiome
func (g *EndGenerator) featureBiome(x, y, z int32) levelgen.EndBiome {
	qx, qy, qz := levelgen.ZoomedQuart(g.zoomSeed, x, y, z)
	return terrain.EndBiomeAt(g.state.Router.Erosion, qx, qy, qz)
}

// endBiomeCacheSize bounds the chunk biomes kept, which each neighbour's
// decoration asks for again.
const endBiomeCacheSize = 8192

// endBiomeCache keeps chunk biomes beyond the central island, whose erosion
// sampling is a 25x25 noise loop.
type endBiomeCache struct {
	mu    sync.Mutex
	byPos map[ChunkPos]levelgen.EndBiome
}

// chunkBiome is the biome a whole chunk carries.
func (g *EndGenerator) chunkBiome(cx, cz int32) levelgen.EndBiome {
	if int64(cx)*int64(cx)+int64(cz)*int64(cz) <= 4096 {
		return levelgen.TheEnd
	}
	pos := ChunkPos{cx, cz}
	g.biomes.mu.Lock()
	b, ok := g.biomes.byPos[pos]
	g.biomes.mu.Unlock()
	if ok {
		return b
	}
	b = terrain.EndBiomeAt(g.state.Router.Erosion, cx<<2, 0, cz<<2)
	g.biomes.mu.Lock()
	if g.biomes.byPos == nil || len(g.biomes.byPos) >= endBiomeCacheSize {
		g.biomes.byPos = make(map[ChunkPos]levelgen.EndBiome, endBiomeCacheSize)
	}
	g.biomes.byPos[pos] = b
	g.biomes.mu.Unlock()
	return b
}

// endBiomeGrid holds the biomes of the 5x5 around a generating chunk, which its
// neighbours' decorations read.
type endBiomeGrid struct {
	cx, cz int32
	biomes [5][5]levelgen.EndBiome
}

func (g *EndGenerator) biomeGrid(cx, cz int32) *endBiomeGrid {
	grid := &endBiomeGrid{cx: cx, cz: cz}
	for dz := range int32(5) {
		for dx := range int32(5) {
			grid.biomes[dz][dx] = g.chunkBiome(cx+dx-2, cz+dz-2)
		}
	}
	return grid
}

// decorate runs chunk (cx, cz)'s features, within one chunk of the grid's
// centre, step by step. A feature runs when its biome is in the surrounding
// 3x3; each placement is then filtered on its own position's biome.
// vanilla: ChunkGenerator.applyBiomeDecoration
func (g *EndGenerator) decorate(r *endRegion, grid *endBiomeGrid, cx, cz int32) {
	g.placeEndCities(r, cx, cz)
	var possible [levelgen.EndBarrens + 1]bool
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			possible[grid.biomes[cz-grid.cz+dz+2][cx-grid.cx+dx+2]] = true
		}
	}
	if !possible[levelgen.SmallEndIslands] && !possible[levelgen.EndHighlands] && !possible[levelgen.TheEnd] {
		return
	}
	ox, oz := cx*sectionSize, cz*sectionSize
	seed := r.rng.SetDecorationSeed(g.seed, ox, oz)
	if possible[levelgen.SmallEndIslands] {
		r.rng.SetFeatureSeed(seed, slotEndIsland.Index, slotEndIsland.Step)
		g.placeEndIslands(r, ox, oz)
	}
	if possible[levelgen.EndHighlands] {
		r.rng.SetFeatureSeed(seed, slotEndGatewayReturn.Index, slotEndGatewayReturn.Step)
		g.placeGatewayReturn(r, ox, oz)
	}
	if possible[levelgen.TheEnd] {
		r.rng.SetFeatureSeed(seed, slotEndSpike.Index, slotEndSpike.Step)
		if g.featureBiome(ox, endMinY, oz) == levelgen.TheEnd {
			g.placeSpikes(r, cx, cz)
		}
	}
	if possible[levelgen.EndHighlands] {
		r.rng.SetFeatureSeed(seed, slotChorusPlant.Index, slotChorusPlant.Step)
		g.placeChorus(r, ox, oz)
	}
	if possible[levelgen.TheEnd] && floorDiv(endArrivalX, sectionSize) == cx && floorDiv(endArrivalZ, sectionSize) == cz &&
		g.featureBiome(endArrivalX, endArrivalY+1, endArrivalZ) == levelgen.TheEnd {
		g.placePlatform(r)
	}
}

// placeGatewayReturn is end_gateway_return: one try in 700 a chunk, at 3..9
// blocks above the motion-blocking heightmap, returning to the arrival platform.
// vanilla: EndPlacements.END_GATEWAY_RETURN, EndGatewayFeature.place
func (g *EndGenerator) placeGatewayReturn(r *endRegion, ox, oz int32) {
	if !(r.rng.NextFloat() < float32(1.0)/700) {
		return
	}
	x := r.rng.NextIntN(sectionSize) + ox
	z := r.rng.NextIntN(sectionSize) + oz
	y := r.motionBlockingHeight(x, z)
	if y <= endMinY {
		return
	}
	y += r.rng.NextIntN(9-3+1) + 3
	if g.featureBiome(x, y, z) != levelgen.EndHighlands {
		return
	}
	for _, c := range EndGatewayPortalCells(x, y, z) {
		r.set(c.X, c.Y, c.Z, c.State)
	}
	r.blockEntities = append(r.blockEntities, nbt.Compound{
		"id": nbt.String("minecraft:end_gateway"),
		"x":  nbt.Int(x), "y": nbt.Int(y), "z": nbt.Int(z),
		"Age":           nbt.Long(0),
		"exit_portal":   nbt.IntArray{100, 50, 0},
		"ExactTeleport": nbt.Byte(1),
	})
}

// placeEndIslands is end_island_decorated: one chunk in 14 grows an island, two
// a quarter of the time, at y 55..70.
// vanilla: EndPlacements.END_ISLAND_DECORATED
func (g *EndGenerator) placeEndIslands(r *endRegion, ox, oz int32) {
	rng := r.rng
	if !(rng.NextFloat() < float32(1.0)/float32(14)) {
		return
	}
	count := int32(1)
	if rng.NextIntN(4) == 3 {
		count = 2
	}
	for range count {
		x := rng.NextIntN(16) + ox
		z := rng.NextIntN(16) + oz
		y := rng.NextIntN(70-55+1) + 55
		if g.featureBiome(x, y, z) != levelgen.SmallEndIslands {
			continue
		}
		endIslandCells(r.rng, x, y, z, func(x, y, z int32) { r.set(x, y, z, g.endStoneState) })
	}
}

// endIslandCells stacks shrinking discs downwards from (ox, oy, oz), handing
// each cell to put.
// vanilla: EndIslandFeature.place
func endIslandCells(rng levelgen.RandomSource, ox, oy, oz int32, put func(x, y, z int32)) {
	size := float32(rng.NextIntN(3)) + 4.0
	for y := oy; size > 0.5; y-- {
		lo, hi := int32(math.Floor(float64(-size))), int32(math.Ceil(float64(size)))
		for x := lo; x <= hi; x++ {
			for z := lo; z <= hi; z++ {
				if float32(x*x+z*z) <= (size+1.0)*(size+1.0) {
					put(ox+x, y, oz+z)
				}
			}
		}
		size -= float32(rng.NextIntN(2)) + 0.5
	}
}

// EndIslandChanges is the island a live world grows at (x, y, z), seeded by
// its origin, as the writes that place it.
// vanilla: ConfiguredFeature.place with RandomSource.create(BlockPos.asLong)
func EndIslandChanges(x, y, z int32) []BlockChange {
	rng := levelgen.NewLegacyRandom(int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF))
	var changes []BlockChange
	endStone := registry.BlockEndStone.DefaultState()
	endIslandCells(rng, x, y, z, func(x, y, z int32) {
		changes = append(changes, BlockChange{X: x, Y: y, Z: z, State: endStone})
	})
	return changes
}

// placeSpikes builds pillars centred in chunk (cx, cz): obsidian from the floor
// to height, air cleared above y=65 over the pillar's square up to 10 above its
// top, a guarded pillar's cage, and the bedrock and fire under its crystal.
// vanilla: EndSpikeFeature.place, EndSpikeFeature.placeSpike
func (g *EndGenerator) placeSpikes(r *endRegion, cx, cz int32) {
	for _, p := range g.pillars {
		if floorDiv(p.x, sectionSize) != cx || floorDiv(p.z, sectionSize) != cz {
			continue
		}
		placeSpike(p, r.set)
		r.rng.NextFloat()
	}
}

// placePlatform is end_platform at (100, 49, 0): obsidian below, three blocks
// of air from it up.
// vanilla: EndPlatformFeature.createEndPlatform
func (g *EndGenerator) placePlatform(r *endRegion) {
	for dz := int32(-2); dz <= 2; dz++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dy := int32(-1); dy < 3; dy++ {
				state := uint16(airState)
				if dy == -1 {
					state = g.obsidianState
				}
				r.set(endArrivalX+dx, endArrivalY+1+dy, endArrivalZ+dz, state)
			}
		}
	}
}

// placeChorus is chorus_plant: 0..4 tries a chunk, each on the MOTION_BLOCKING
// surface of a highlands column.
// vanilla: EndPlacements.CHORUS_PLANT, ChorusPlantFeature.place
func (g *EndGenerator) placeChorus(r *endRegion, ox, oz int32) {
	rng := r.rng
	count := rng.NextIntN(5)
	for range count {
		x := rng.NextIntN(16) + ox
		z := rng.NextIntN(16) + oz
		y := r.motionBlockingHeight(x, z)
		if y <= endMinY || g.featureBiome(x, y, z) != levelgen.EndHighlands {
			continue
		}
		if r.isAir(x, y, z) && g.supportsChorus(r.get(x, y-1, z)) {
			g.setChorusPlant(r, x, y, z)
			g.growChorus(r, x, y, z, x, z, 0)
		}
	}
}

// chorusStates holds the chorus block states the growth places, resolved once.
type chorusStates struct {
	chorusFlower uint16
	// chorusPlants holds the chorus plant states indexed by down, up, north,
	// east, south, west connections as bits 0..5, all within
	// chorusPlantMin..chorusPlantMax.
	chorusPlants                   [64]uint16
	chorusPlantMin, chorusPlantMax uint16
}

func newChorusStates() chorusStates {
	var c chorusStates
	flower, ok := registry.WithInt(registry.BlockChorusFlower.DefaultState(), registry.PropAge, 5)
	if !ok {
		panic("chorus flower has no age 5")
	}
	c.chorusFlower = flower
	c.chorusPlants, c.chorusPlantMin, c.chorusPlantMax = chorusPlantStates()
	return c
}

// chorusRegion is the block access the chorus growth needs, from the End's
// generation region or a live world's decoration region.
type chorusRegion interface {
	get(x, y, z int32) uint16
	set(x, y, z int32, state uint16)
	isAir(x, y, z int32) bool
	random() *levelgen.WorldgenRandom
}

func (r *endRegion) random() *levelgen.WorldgenRandom   { return r.rng }
func (r *decorRegion) random() *levelgen.WorldgenRandom { return r.rng }

var liveChorus = sync.OnceValue(newChorusStates)

// placeLiveChorus is ChorusPlantFeature.place: a plant on a supporting block
// grown into a tree.
// vanilla: ChorusPlantFeature.place
func placeLiveChorus(r *decorRegion, p treePos) bool {
	c := liveChorus()
	x, y, z := p[0], p[1], p[2]
	if !r.isAir(x, y, z) || !c.supportsChorus(r.get(x, y-1, z)) {
		return false
	}
	c.setChorusPlant(r, x, y, z)
	c.growChorus(r, x, y, z, x, z, 0)
	return true
}

func (g *chorusStates) supportsChorus(state uint16) bool {
	return stateInTag(state, registry.TagIDSupportsChorusPlant)
}

// setChorusPlant places a chorus plant joined to its current neighbours;
// vanilla never revisits it once the tree is done.
// vanilla: ChorusPlantBlock.getStateWithConnections
func (g *chorusStates) setChorusPlant(r chorusRegion, x, y, z int32) {
	joins := func(s uint16) bool { return s == g.chorusFlower || g.isChorusPlant(s) }
	var bits int
	for i, on := range [6]bool{
		joins(r.get(x, y-1, z)) || g.supportsChorus(r.get(x, y-1, z)),
		joins(r.get(x, y+1, z)),
		joins(r.get(x, y, z-1)),
		joins(r.get(x+1, y, z)),
		joins(r.get(x, y, z+1)),
		joins(r.get(x-1, y, z)),
	} {
		if on {
			bits |= 1 << i
		}
	}
	r.set(x, y, z, g.chorusPlants[bits])
}

func (g *chorusStates) isChorusPlant(s uint16) bool {
	return s >= g.chorusPlantMin && s <= g.chorusPlantMax
}

// endChorusNeighborsEmpty reports whether the four horizontal neighbours of a
// block are air, but for the one in direction ignore (-1 for none).
// vanilla: ChorusFlowerBlock.allNeighborsEmpty
func endChorusNeighborsEmpty(r chorusRegion, x, y, z int32, ignore int) bool {
	for _, dir := range chorusHorizontalDirs {
		if dir == ignore {
			continue
		}
		d := dirDelta[dir]
		if !r.isAir(x+d[0], y, z+d[2]) {
			return false
		}
	}
	return true
}

// growChorus grows one stem: a column up, then branches each growing their own
// stem, or a dead flower on top if none took.
// vanilla: ChorusFlowerBlock.growTreeRecursive
func (g *chorusStates) growChorus(r chorusRegion, x, y, z, startX, startZ int32, depth int) {
	rng := r.random()
	height := rng.NextIntN(4) + 1
	if depth == 0 {
		height++
	}
	for i := range height {
		ty := y + i + 1
		if !endChorusNeighborsEmpty(r, x, ty, z, -1) {
			return
		}
		g.setChorusPlant(r, x, ty, z)
		g.setChorusPlant(r, x, ty-1, z)
	}
	placedStem := false
	if depth < 4 {
		stems := rng.NextIntN(4)
		if depth == 0 {
			stems++
		}
		for range stems {
			dir := chorusHorizontalDirs[rng.NextIntN(4)]
			d := dirDelta[dir]
			tx, ty, tz := x+d[0], y+height, z+d[2]
			if absI32(tx-startX) >= endChorusSpread || absI32(tz-startZ) >= endChorusSpread ||
				!r.isAir(tx, ty, tz) || !r.isAir(tx, ty-1, tz) ||
				!endChorusNeighborsEmpty(r, tx, ty, tz, blocklogic.Opposite(dir)) {
				continue
			}
			placedStem = true
			g.setChorusPlant(r, tx, ty, tz)
			g.setChorusPlant(r, tx-d[0], ty, tz-d[2])
			g.growChorus(r, tx, ty, tz, startX, startZ, depth+1)
		}
	}
	if !placedStem {
		r.set(x, y+height, z, g.chorusFlower)
	}
}

// chorusPlantStates resolves the 64 chorus plant states (down, up, north, east,
// south, west as bits 0..5) and their range.
func chorusPlantStates() (states [64]uint16, lo, hi uint16) {
	lo, hi = math.MaxUint16, 0
	for i := range states {
		s := registry.BlockChorusPlant.DefaultState()
		for bit, p := range [...]registry.Prop{registry.PropDown, registry.PropUp, registry.PropNorth, registry.PropEast, registry.PropSouth, registry.PropWest} {
			var ok bool
			if s, ok = registry.WithBool(s, p, i&(1<<bit) != 0); !ok {
				panic("chorus plant has no " + strconv.Itoa(bit) + "th side property")
			}
		}
		states[i] = s
		lo, hi = min(lo, s), max(hi, s)
	}
	return states, lo, hi
}

// BlockAt reads the End decoration region for shared placement and shape rules.
func (r *endRegion) BlockAt(x, y, z int32) uint16 { return r.get(x, y, z) }

func placeSpike(p endPillar, set func(int32, int32, int32, uint16)) {
	rr := p.radius
	for x := p.x - rr; x <= p.x+rr; x++ {
		for z := p.z - rr; z <= p.z+rr; z++ {
			dx, dz := x-p.x, z-p.z
			inside := dx*dx+dz*dz <= rr*rr+1
			for y := int32(endMinY); y <= p.top+10; y++ {
				switch {
				case inside && y < p.top:
					set(x, y, z, registry.BlockObsidian.DefaultState())
				case y > 65:
					set(x, y, z, airState)
				}
			}
		}
	}
	if p.guarded {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				for dy := int32(0); dy <= 3; dy++ {
					top := dy == 3
					if dx != -2 && dx != 2 && dz != -2 && dz != 2 && !top {
						continue
					}
					xEdge := dx == -2 || dx == 2 || top
					zEdge := dz == -2 || dz == 2 || top
					var bars int
					for bit, on := range [...]bool{xEdge && dz != -2, xEdge && dz != 2, zEdge && dx != -2, zEdge && dx != 2} {
						if on {
							bars |= 1 << bit
						}
					}
					set(p.x+dx, p.top+dy, p.z+dz, spikeCageBars[bars])
				}
			}
		}
	}
	set(p.x, p.top, p.z, registry.BlockBedrock.DefaultState())
	set(p.x, p.top+1, p.z, registry.BlockFire.DefaultState())
}

// EndSpikeRebuild returns a spike's blocks and crystal feet position.
// vanilla: DragonRespawnStage.SUMMONING_PILLARS
func EndSpikeRebuild(seed int64, index int) ([]BlockChange, [3]int32) {
	p := endSpikes(seed)[index]
	var changes []BlockChange
	placeSpike(p, func(x, y, z int32, state uint16) {
		changes = append(changes, BlockChange{X: x, Y: y, Z: z, State: state})
	})
	return changes, [3]int32{p.x, p.top + 1, p.z}
}

// EndSpikeTop returns the crystal feet block for one spike.
func EndSpikeTop(seed int64, index int) [3]int32 {
	p := endSpikes(seed)[index]
	return [3]int32{p.x, p.top + 1, p.z}
}
