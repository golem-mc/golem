package world

import (
	"maps"
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const (
	sectionSize      = 16
	blocksPerSection = sectionSize * sectionSize * sectionSize

	// Paletted container widths: a uniform container carries its one value and
	// no data array; otherwise a palette indexed at 4-8 bits per entry. Past
	// 256 entries the client expects global state IDs at the width its block
	// registry needs (15 for vanilla).
	minIndirectBits = 4
	maxIndirectBits = 8
	directBits      = 16

	// A section's biome grid is one biome per 4x4x4 cells, indexed like the
	// block array; its indirect palette runs 1-3 bits, past which it carries
	// raw registry IDs.
	// vanilla: LevelChunkSection.BIOME_CONTAINER_BITS = 2
	minBiomeBits     = 1
	maxBiomeBits     = 3
	biomesPerSection = 4 * 4 * 4
)

// The starting biome and the biome ID width come from the registry sent to the
// client, not constants, so a version change moving either is picked up by the
// regen.
var (
	// plainsBiome is a fresh biome container's default, what a chunk and its
	// sections start with.
	// vanilla: PalettedContainerFactory.createForBiomes
	plainsBiome = registry.MustID("minecraft:worldgen/biome", "minecraft:plains")
	// biomeDirectBits is the direct biome container's width: the bits the biome
	// registry needs.
	// vanilla: Strategy.globalPaletteBitsInMemory
	biomeDirectBits = func() int {
		reg, _ := registry.Get("minecraft:worldgen/biome")
		return bits.Len(uint(max(len(reg.Entries), 2) - 1))
	}()
)

// Chunk is a 16x16 column from worldMinY to the build limit, the unit of
// generation and of the protocol, split into the 16-block sections Chunk Data
// carries.
type Chunk struct {
	// generatedEntities hold starting entities until wake/save takes responsibility.
	generatedEntities []nbt.Compound
	// generationWorld supplies game rules while Generate is building this chunk.
	generationWorld *World

	x, z int32

	sections [sectionCount]section
	// shared is set, under the world's write lock, when the chunk joins a
	// world, where World.BlockAt reads its blocks with no lock. From then on a
	// block write stores its word atomically and a section's fill stays fixed.
	shared bool
	// biome is the column's default, which generation sets and sections start
	// sharing. Sections' own cells are what wire and disk carry, so a loaded
	// chunk keeps plains here whatever its sections say.
	biome int32

	// dataVersion is the DataVersion the file claimed, zero for none (a
	// generated chunk, or a missing or zero field, which level.dat's rule reads
	// alike). A save keeps the claim (claimedVersion), so data carried unread
	// from an older file still tells the game to upgrade it.
	dataVersion int32
	// randomTickSections indexes sections with ticking > 0 in a shared chunk,
	// under the same world lock as the counts.
	randomTickSections uint32

	// inhabitedTime is the ticks players have spent near the chunk, the local
	// share of a spawn's difficulty; the tick pass ages every chunk near a
	// player by one.
	// vanilla: ChunkAccess.incrementInhabitedTime
	inhabitedTime int64

	// pluginData is private saved data, guarded by the world lock.
	pluginData nbt.Compound

	// carriedBlockEntities holds the block entities of kinds this server does
	// not model and carriedEntities the chunk's mobs, carried from the file so
	// a vanilla world keeps them. A block entity created here goes in the
	// carried list only when a command gives one to a block whose entity is not
	// modelled, never in carriedEntities.
	//
	// entities holds the live block entities keyed by block position, and
	// entityOrder those positions in insertion order, the order a save writes
	// so two saves of a chunk give the same bytes. The tick order spans chunks
	// (World.blockEntityOrder).
	//
	// pistonRides holds active moving-piston rides separately so piston moves
	// do not allocate a full BlockEntity; guarded by World.mu.
	pistonRides map[int64]pistonRide
	// pistonHandles is allocated only when a plugin requests a live handle.
	pistonHandles        map[int64]*PistonBlockEntity
	carriedBlockEntities nbt.List
	// carriedHandles is allocated only when a plugin requests a live handle.
	carriedHandles map[int64]*CarriedBlockEntity
	// tickingCarried counts the trial spawners, vaults, beehives and end gateways among carriedBlockEntities.
	tickingCarried  int
	carriedEntities nbt.List

	// levelEntities are the entities a chunk in the layout before 1.17 held,
	// upgraded; loadChunk moves them to the entity store.
	levelEntities []nbt.Tag
	// carriedRoot is the unmodelled root tags (structure starts,
	// PostProcessing, carving_mask, UpgradeData and the rest), kept as read so
	// a save does not drop a vanilla world's villages and strongholds.
	// LastUpdate and InhabitedTime are modelled and the tick lists left out, so
	// none of those are carried.
	carriedRoot nbt.Compound
	// liveStarts are the structure starts in carriedRoot that change after
	// generation, by id; savedRoot writes them as they stand.
	liveStarts map[string]func() nbt.Compound

	// loadedTicks holds scheduled ticks from disk or generation until the
	// world hands them to the physics engine on load.
	loadedTicks []loadedTick

	// lastUpdate is the game time of the last save, LastUpdate on disk; a
	// generated chunk starts at zero and takes the saving world's time.
	// vanilla: SerializableChunkData.lastUpdateTime
	lastUpdate  int64
	entities    map[int64]*BlockEntity
	entityOrder []int64

	// pendingGenerated holds the blocks (fluid sources, eyeblossoms) a generator scheduled a tick for, in
	// placement order, including aquifer and waterlogged post-processing visits.
	// SpringFeature.place schedules the tick as it places the
	// source, so a spring pours on the chunk's first tick. The world hands them
	// to physics on load. x and z are chunk-local, y a world coordinate.
	pendingGenerated []posKey

	// encoded is the sections' wire form, built on generation or load. It is
	// replaced whole, never edited, so players can share it. stale marks a
	// write it has not caught up with; ChunkPacket rebuilds it.
	encoded []byte
	// packetMu serializes packet snapshots, taken before World.mu and never
	// while holding a block entity lock. Packet builds release World.mu before
	// reading update tags, following the entity-before-world lock order.
	packetMu sync.Mutex
	packet   chunkPacketCache
	// heightmaps is the heightmaps' wire form, rebuilt with encoded.
	heightmaps []byte
	stale      bool
	// writes counts stale marks, so a form built under the read lock is kept
	// only if no write landed meanwhile.
	writes uint64

	// skyLight and blockLight hold the per-section light nibbles by light
	// section (0 below the world, 1..sectionCount inside, sectionCount+1
	// above); nil is all zero. lightEncoded is the wire form, rebuilt when the
	// arrays change; chunkLightEncoded is the whole-chunk send's form, omitting
	// uniformly lit sky sections. lightCorrect marks light computed and safe to
	// cache on disk.
	//
	// Once the chunk is in a world its arrays are never written in place: a
	// relight writes copies (lightShadow, lightJoin) and swaps them in under
	// the write lock, so a shadow reads the live arrays with no lock.
	skyLight          [lightSections]*[lightArrayLen]byte
	blockLight        [lightSections]*[lightArrayLen]byte
	lightEncoded      []byte
	chunkLightEncoded []byte
	// A nil encoded form is out of date and built on first read of that form
	// (lightForm, chunkLightForm), under lightFormsMu because readers hold
	// w.mu only for reading. Cache reads also take lightFormsMu; writes hold
	// either w.mu for writing or w.mu for reading + lightFormsMu. The forms
	// are replaced, never edited. lightFormsInit is set once the chunk has
	// light, so an evicted form is told from a never-lit chunk.
	lightFormsInit bool
	lightFormsMu   sync.Mutex
	// lightFormsRetained is set only while the packet cache owns this chunk.
	lightFormsRetained bool
	lightCorrect       bool
	// lightBorrowed marks, per layer and light section, an array shared with
	// another chunk or a uniform constant, copied before its first write
	// (writableLight).
	lightBorrowed [2]uint32
	// lightGen counts light writes while loaded, so a relight computed on a
	// copy (lightShadow) is installed only if no other light landed meanwhile.
	lightGen uint64
	// lightDirty marks, per local x, the z columns where a write changed a
	// state the light engine reads differently since the last relight covering
	// them; relighting unmarked columns would recompute the same light.
	lightDirty [sectionSize]uint16
	// lightCells lists the cells behind lightDirty (markLightCell) so a relight
	// can start from them rather than whole columns; lightOverflow is set once
	// a write did not fit and the marks no longer name every cell.
	lightCells    []uint32
	lightOverflow bool
	lightBands    uint32 // section mask, retained even when lightCells overflows
}

// X returns the chunk's X coordinate.
func (c *Chunk) X() int32 { return c.x }

// Z returns the chunk's Z coordinate.
func (c *Chunk) Z() int32 { return c.z }

// BiomeAt is the biome of the 4x4x4 cell holding the block (x, z chunk-local, y
// world), the grid a section's biome container covers. A Y outside the world
// reads the column default.
func (c *Chunk) BiomeAt(x, y, z int32) int32 {
	if !contains(y) || x < 0 || x >= sectionSize || z < 0 || z >= sectionSize {
		return c.biome
	}
	local := y - worldMinY
	return c.sections[local/sectionSize].cellBiome(biomeCell(x, local%sectionSize, z))
}

// section is a 16x16x16 cube. It starts uniform and grows a block array only
// when a block differs, which keeps a flat world's chunks to a few hundred
// bytes; the biome grid works the same at four blocks a cell.
type section struct {
	fill  uint16
	spawn atomic.Pointer[spawnCollision]
	// tickMask is the lazily built random-ticking cell mask (randomtick_cells.go);
	// nil means unbuilt. Reset wherever ticking is recounted.
	tickMask atomic.Pointer[randomTickMask]
	// blocks is nil while every block is fill. Shared chunks publish a new
	// layout atomically when the palette outgrows its entry width.
	blocks    atomic.Pointer[blockWords]
	biome     int32                         // the section's one biome while biomes is nil
	biomes    []int32                       // the 4x4x4 biome cells, nil while the section is uniform
	biomeGrid atomic.Pointer[biomeSnapshot] // immutable raw cells for lock-free zoom reads

	// ticking counts the section's random-ticking states
	// (LevelChunkSection.tickingBlockCount), so the pass rolls no dice over a
	// section with nothing to tick. set and setAll keep it current; a
	// whole-section decode recounts it.
	ticking int
	// emitting counts states with light emission above zero, so the emitter
	// scan skips sections with none.
	emitting int
	// sculk indexes listeners and catalysts under the world lock on shared chunks.
	sculk *sectionSculkCells
	// colliding counts states with a possible entity collision, including
	// body-dependent and moving shapes.
	colliding int
	// rods counts lightning rods, so the rod search skips sections with none.
	rods int
	// eggs counts turtle eggs, so a zombie's egg search skips sections with none.
	eggs int
	// portals and pois let destination and village scans skip empty sections.
	portals int
	pois    int
	// touches is read through the near cache without the world lock.
	touches atomic.Int32
	// shapes counts blocks the server's shape reconnect pass reacts to; read
	// like touches.
	shapes atomic.Int32
	// comparators is guarded by the world lock once the chunk is shared.
	comparators int32
}

// newChunk returns an empty chunk at the chunk coordinates for a generator to
// fill. Its sections start with a fresh biome container's plains, what a
// section the disk names no biome for reads as.
// vanilla: PalettedContainerFactory.createForBiomes
func newChunk(x, z int32) *Chunk {
	c := &Chunk{x: x, z: z, biome: plainsBiome}
	for i := range c.sections {
		c.sections[i].fill = airState
		c.sections[i].biome = plainsBiome
	}
	return c
}

// blockIndex is the index of section-local coordinates in the block array and
// the wire's data array: x fastest, then z, then y.
func blockIndex(x, y, z int32) int {
	return int(y<<8 | z<<4 | x)
}

// biomeCell is the 4x4x4 biome cell a section-local coordinate falls in,
// ordered like blockIndex.
// vanilla: Strategy.getIndex
func biomeCell(x, y, z int32) int {
	return int(y>>2<<4 | z>>2<<2 | x>>2)
}

// quartBiome is the biome cell of quart (lqx, qy, lqz): lqx and lqz chunk-local
// in 0..3, qy a world quart inside the world. It skips BiomeAt's range checks.
func (c *Chunk) quartBiome(lqx, qy, lqz int32) int32 {
	local := qy - worldMinY>>2
	s := &c.sections[local>>2]
	if s.biomes == nil {
		return s.biome
	}
	return s.biomes[(local&3)<<4|lqz<<2|lqx]
}

// contains reports whether a Y coordinate is inside the world.
func contains(y int32) bool {
	return y >= worldMinY && y < worldMinY+worldHeight
}

// SetBlock sets a block (x, z chunk-local, y world); coordinates outside the
// chunk are ignored, as when a generator runs off the world's top or bottom.
func (c *Chunk) SetBlock(x, y, z int32, state uint16) {
	if !contains(y) || x < 0 || x >= sectionSize || z < 0 || z >= sectionSize {
		return
	}
	y -= worldMinY
	s, i := &c.sections[y/sectionSize], blockIndex(x, y%sectionSize, z)
	c.setIndexed(s, i, x, y, z, state, registry.SameLight(s.at(i), state))
}

// setIndexed is SetBlock for a caller that has located the cell (s, i is x, y,
// z with y relative to worldMinY) and knows whether state shares the light
// properties of the block there.
func (c *Chunk) setIndexed(s *section, i int, x, y, z int32, state uint16, sameLight bool) {
	if !sameLight {
		c.lightDirty[x] |= 1 << z
		c.markLightCell(x, y, z)
	}
	wasTicking := c.shared && s.ticking > 0
	s.set(i, state, c.shared)
	if c.shared && (s.ticking > 0) != wasTicking {
		c.setRandomTickSection(y/sectionSize, s.ticking > 0)
	}
}

func (c *Chunk) setRandomTickSection(si int32, ticking bool) {
	if ticking {
		c.randomTickSections |= 1 << si
	} else {
		c.randomTickSections &^= 1 << si
	}
}

// rebuildRandomTickSections derives the index from the generated or decoded counts.
func (c *Chunk) rebuildRandomTickSections() {
	c.randomTickSections = 0
	for i := range c.sections {
		if c.sections[i].ticking > 0 {
			c.randomTickSections |= 1 << i
		}
	}
}

// scheduleGenerated records a fluid source whose tick the generator scheduled, for
// the world to hand to physics on load.
// vanilla: SpringFeature.place, NoiseBasedChunkGenerator.doFill
func (c *Chunk) scheduleGenerated(x, y, z int32) {
	c.pendingGenerated = append(c.pendingGenerated, posKey{x, y, z})
}

// BlockAt returns a block (x, z chunk-local, y world); outside the chunk is
// air.
func (c *Chunk) BlockAt(x, y, z int32) uint16 {
	// A negative x or z, or a y that wraps past either end of int32,
	// compares out of range.
	ly := uint32(y - worldMinY)
	if ly >= worldHeight || uint32(x|z) >= sectionSize {
		return airState
	}
	s := &c.sections[ly>>4]
	if b := s.blocks.Load(); b != nil {
		return b.get(int(ly&15<<8 | uint32(z)<<4 | uint32(x)))
	}
	return s.fill
}

// sharedBlockAt is BlockAt for a reader holding no lock on a shared chunk's
// arrays (World.BlockAt, the light engine on a shadow's views), x and z
// already chunk-local (x&15).
func (c *Chunk) sharedBlockAt(x, y, z int32) uint16 {
	ly := uint32(y - worldMinY)
	if ly >= worldHeight {
		return airState
	}
	s := &c.sections[ly>>4]
	if b := s.blocks.Load(); b != nil {
		return b.load(int(ly&15<<8 | uint32(z)<<4 | uint32(x)))
	}
	return s.fill
}

// Fill sets every block with fromY <= y < toY to state; fully covered sections
// of a chunk not yet shared are filled whole, so a flat chunk allocates no
// block array.
func (c *Chunk) Fill(fromY, toY int32, state uint16) {
	fromY = max(fromY, worldMinY)
	toY = min(toY, worldMinY+worldHeight)

	for y := fromY; y < toY; {
		si := (y - worldMinY) / sectionSize
		s := &c.sections[si]
		baseY := worldMinY + si*sectionSize
		endY := min(toY, baseY+sectionSize)
		if y == baseY && endY-y == sectionSize && !c.shared {
			s.setAll(state)
			y = endY
			continue
		}
		for ; y < endY; y++ {
			for z := range int32(sectionSize) {
				for x := range int32(sectionSize) {
					s.set(blockIndex(x, y-baseY, z), state, c.shared)
				}
			}
		}
		if c.shared {
			c.setRandomTickSection(si, s.ticking > 0)
		}
	}
}

// SetBiome sets the whole column's biome, propagated to every section since
// wire and disk carry it per section.
func (c *Chunk) SetBiome(biome int32) {
	c.biome = biome
	for i := range c.sections {
		c.sections[i].biome = biome
		c.sections[i].biomes = nil
		c.sections[i].biomeGrid.Store(nil)
	}
}

// SetCellBiome sets the biome of the 4x4x4 cell holding the block (x, z
// chunk-local, y world), BiomeAt's write side. A value matching the section's
// keeps it uniform.
func (c *Chunk) SetCellBiome(x, y, z int32, biome int32) {
	if !contains(y) || x < 0 || x >= sectionSize || z < 0 || z >= sectionSize {
		return
	}
	local := y - worldMinY
	c.sections[local/sectionSize].setCell(biomeCell(x, local%sectionSize, z), biome)
}

// at returns the block at an index within the section.
func (s *section) at(i int) uint16 {
	if b := s.blocks.Load(); b != nil {
		return b.get(i)
	}
	return s.fill
}

// load is at for a reader holding no lock on a shared chunk, whose fill no
// longer changes.
func (s *section) load(i int) uint16 {
	if b := s.blocks.Load(); b != nil {
		return b.load(i)
	}
	return s.fill
}

// isUniform reports whether every block of the section is state.
func (s *section) isUniform(state uint16) bool {
	return s.blocks.Load() == nil && s.fill == state
}

// set puts a block at an index, giving the section its own block array if
// needed; shared is the chunk's.
func (s *section) set(i int, state uint16, shared bool) {
	old := s.at(i)
	if old == state {
		return
	}
	if cache := s.spawn.Load(); cache != nil && spawnCollisionBits(old) != spawnCollisionBits(state) {
		if cache == &spawnAll || cache == &spawnNone {
			s.spawn.Store(nil)
		} else if shift := 2 * (i & 31); cache[i>>5].Load()>>shift&3 != 0 {
			word := cache[i>>5].Load()
			cache[i>>5].Store(word&^(3<<shift) | spawnCollisionBits(state)<<shift)
		}
	}
	if b := s.blocks.Load(); b != nil {
		if next := b.put(i, state, shared); next != b {
			s.blocks.Store(next)
		}
	} else {
		// Filled before it is stored, so a lock-free reader that loads the
		// array sees every index.
		b = newBlockWords(s.fill)
		b = b.put(i, state, false)
		s.blocks.Store(b)
	}
	if newTick, oldTick := randomlyTicking(state), randomlyTicking(old); newTick != oldTick {
		if m := s.tickMask.Load(); m != nil {
			m.flip(i)
		}
		if newTick {
			s.ticking++
		} else {
			s.ticking--
		}
	}
	if registry.LightEmission(state) > 0 {
		s.emitting++
	}
	if registry.LightEmission(old) > 0 {
		s.emitting--
	}
	if MayCollide(state) {
		s.colliding++
	}
	if MayCollide(old) {
		s.colliding--
	}
	if isLightningRod(state) {
		s.rods++
	}
	if isLightningRod(old) {
		s.rods--
	}
	if IsTurtleEgg(state) {
		s.eggs++
	}
	if IsTurtleEgg(old) {
		s.eggs--
	}
	block, oldBlock := registry.BlockOf(state), registry.BlockOf(old)
	if kind, was := sculkCellKindOf(block), sculkCellKindOf(oldBlock); kind != was {
		s.updateSculkCell(uint16(i), kind, was)
	}
	switch block {
	case registry.BlockNetherPortal:
		s.portals++
	case registry.BlockComparator:
		s.comparators++
	}
	switch oldBlock {
	case registry.BlockNetherPortal:
		s.portals--
	case registry.BlockComparator:
		s.comparators--
	}
	if registry.IsPOI(state) {
		s.pois++
	}
	if registry.IsPOI(old) {
		s.pois--
	}
	if isTouchBlock[block] {
		s.touches.Add(1)
	}
	if isTouchBlock[oldBlock] {
		s.touches.Add(-1)
	}
	if shape, was := isShapeBlock[block], isShapeBlock[oldBlock]; shape != was {
		if shape {
			s.shapes.Add(1)
		} else {
			s.shapes.Add(-1)
		}
	}
}

// setAll makes the whole section one block, dropping any block array it had.
// It changes fill, so it is only for a chunk not yet shared.
func (s *section) setAll(state uint16) {
	s.spawn.Store(nil)
	s.fill = state
	s.blocks.Store(nil)
	s.recalcCounts()
}

// recalcCounts rebuilds section counts after bulk block replacement. It
// classifies each distinct state once with its multiplicity
// (vanilla: LevelChunkSection.recalcBlockCounts).
func (s *section) recalcCounts() {
	s.tickMask.Store(nil)
	s.ticking, s.emitting, s.rods, s.eggs, s.portals, s.pois = 0, 0, 0, 0, 0, 0
	s.sculk = nil
	s.touches.Store(0)
	s.shapes.Store(0)
	s.colliding, s.comparators = 0, 0
	var touches, shapes int32
	var sculk [sculkCellKinds]int
	blocks := s.blocks.Load()
	switch {
	case blocks == nil:
		if kind := s.countState(s.fill, blocksPerSection, &touches, &shapes); kind != noSculkCell {
			cells := make([]uint16, blocksPerSection)
			for i := range cells {
				cells[i] = uint16(i)
			}
			*s.sculkList(kind) = cells
		}
	case blocks.palette == nil:
		for i := range blocksPerSection {
			if kind := s.countState(blocks.get(i), 1, &touches, &shapes); kind != noSculkCell {
				cells := s.sculkList(kind)
				*cells = append(*cells, uint16(i))
			}
		}
	default:
		// Same walk as blockWords.unpack, so a padded last word is ignored.
		var freq [1 << maxIndirectBits]uint16
		i := 0
		for _, word := range blocks.words {
			for j := 0; j < blocks.perWord && i < blocksPerSection; j++ {
				freq[word&blocks.mask]++
				word >>= blocks.bits
				i++
			}
		}
		for slot := range min(blocks.used, len(freq)) {
			if n := freq[slot]; n > 0 {
				if kind := s.countState(uint16(blocks.palette[slot]), int(n), &touches, &shapes); kind != noSculkCell {
					sculk[kind] += int(n)
				}
			}
		}
		if sculk != [sculkCellKinds]int{} {
			s.indexDecodedSculkCells(blocks, sculk)
		}
	}
	s.touches.Store(touches)
	s.shapes.Store(shapes)
}

// countState adds n cells of state to the counters. It reports whether the
// state's sculk cell kind, or noSculkCell; the caller lists those cells.
func (s *section) countState(state uint16, n int, touches, shapes *int32) sculkCellKind {
	if randomlyTicking(state) {
		s.ticking += n
	}
	if registry.LightEmission(state) > 0 {
		s.emitting += n
	}
	if MayCollide(state) {
		s.colliding += n
	}
	if isLightningRod(state) {
		s.rods += n
	}
	if IsTurtleEgg(state) {
		s.eggs += n
	}
	block := registry.BlockOf(state)
	switch block {
	case registry.BlockNetherPortal:
		s.portals += n
	case registry.BlockComparator:
		s.comparators += int32(n)
	}
	if registry.IsPOI(state) {
		s.pois += n
	}
	if isTouchBlock[block] {
		*touches += int32(n)
	}
	if isShapeBlock[block] {
		*shapes += int32(n)
	}
	return sculkCellKindOf(block)
}

// scratchBuffers pools the buffers a payload is built in before it is copied
// out at its size; a scratch's bytes are never kept past that copy.
var scratchBuffers = sync.Pool{New: func() any { return new(protocol.Buffer) }}

// scratchBuffer takes an empty buffer from scratchBuffers.
func scratchBuffer() *protocol.Buffer {
	b := scratchBuffers.Get().(*protocol.Buffer)
	b.Reset()
	return b
}

// encode writes a single chunk section.
func (s *section) encode(b *protocol.Buffer) {
	if blocks := s.blocks.Load(); blocks == nil {
		var nonAir, fluids int16
		if s.fill != airState {
			nonAir = blocksPerSection
		}
		if registry.FluidStates[s.fill] {
			fluids = blocksPerSection
		}
		b.PutI16(nonAir)
		b.PutI16(fluids)
		b.PutU8(0)
		b.PutVarInt(int32(s.fill))
	} else {
		// Decode once for the counts, canonical palette and wire repacking.
		var states [blocksPerSection]uint16
		blocks.unpack(&states)
		var nonAir, fluids int16
		for _, state := range states {
			if state != airState {
				nonAir++
			}
			if registry.FluidStates[state] {
				fluids++
			}
		}
		b.PutI16(nonAir)
		b.PutI16(fluids)
		putBlockStates(b, &states)
	}
	s.putBiomes(b)
}

// putBlockStates preserves first-seen palette order, independent of storage's
// palette insertion order and any slots no longer in use.
func putBlockStates(b *protocol.Buffer, states *[blocksPerSection]uint16) {
	palette, slots := paletteOf(states)
	defer releasePalette(palette, slots)
	switch {
	case len(palette) == 1:
		b.PutU8(0)
		b.PutVarInt(int32(palette[0]))
	case len(palette) <= 1<<maxIndirectBits:
		entryBits := max(minIndirectBits, bits.Len(uint(len(palette)-1)))
		b.PutU8(uint8(entryBits))
		b.PutVarInt(int32(len(palette)))
		for _, state := range palette {
			b.PutVarInt(int32(state))
		}
		if entryBits == 4 {
			const entriesPerWord = 64 / 4
			for i := 0; i < len(states); i += entriesPerWord {
				group := states[i : i+entriesPerWord]
				var word uint64
				// Build backwards so the first cell occupies the low nibble.
				for _, g := range slices.Backward(group) {
					word = word<<4 | uint64(slots[g]-1)
				}
				b.PutI64(int64(word))
			}
			return
		}
		putPacked(b, entryBits, blocksPerSection, func(i int) uint64 { return uint64(slots[states[i]] - 1) })
	default:
		b.PutU8(directBits)
		putPacked(b, directBits, blocksPerSection, func(i int) uint64 { return uint64(states[i]) })
	}
}

// cellBiome reads the biome of one cell of the section's grid.
func (s *section) cellBiome(cell int) int32 {
	if s.biomes == nil {
		return s.biome
	}
	return s.biomes[cell]
}

// setCell sets one biome cell, growing the cell array only where the value
// differs from the uniform biome.
func (s *section) setCell(cell int, biome int32) {
	if s.biomes == nil {
		if biome == s.biome {
			return
		}
		s.biomes = make([]int32, biomesPerSection)
		for i := range s.biomes {
			s.biomes[i] = s.biome
		}
	}
	s.biomes[cell] = biome
	s.biomeGrid.Store(nil)
}

// settleBiomes drops a cell array holding one biome, so a generator writing
// whole bands leaves a uniform section and wire and disk carry the single
// value.
func (s *section) settleBiomes() {
	if s.biomes == nil {
		return
	}
	for _, biome := range s.biomes {
		if biome != s.biomes[0] {
			return
		}
	}
	s.biome, s.biomes = s.biomes[0], nil
	s.biomeGrid.Store(nil)
}

// putBiomes writes the 64-cell biome grid: uniform as its one biome, mixed as a
// palette and packed indices, or raw IDs once more biomes appear than the 3-bit
// palette holds.
// vanilla: PalettedContainer.Data.write, Strategy.createForBiomes
func (s *section) putBiomes(b *protocol.Buffer) {
	if s.biomes == nil {
		b.PutU8(0)
		b.PutVarInt(s.biome)
		return
	}

	var paletteBuf [biomesPerSection]int32
	var index [biomesPerSection]uint16
	palette := paletteBuf[:s.biomePalette(&paletteBuf, &index)]
	if len(palette) <= 1<<maxBiomeBits {
		entryBits := max(minBiomeBits, bits.Len(uint(len(palette)-1)))
		b.PutU8(uint8(entryBits))
		b.PutVarInt(int32(len(palette)))
		for _, biome := range palette {
			b.PutVarInt(biome)
		}
		putPacked(b, entryBits, len(s.biomes), func(i int) uint64 {
			return uint64(index[i])
		})
		return
	}

	b.PutU8(uint8(biomeDirectBits))
	putPacked(b, biomeDirectBits, len(s.biomes), func(i int) uint64 {
		return uint64(s.biomes[i])
	})
}

// biomePalette fills palette with the section's distinct biomes in first-seen
// order and index with each cell's palette index, returning the palette's
// length. IDs outside the registry are found by a linear search of the palette.
func (s *section) biomePalette(palette *[biomesPerSection]int32, index *[biomesPerSection]uint16) int {
	var slots [registry.BiomeCount]uint8 // palette index plus one; zero if absent
	n := 0
	for cell, biome := range s.biomes {
		var slot int
		if uint32(biome) < registry.BiomeCount {
			if slots[biome] == 0 {
				palette[n] = biome
				n++
				slots[biome] = uint8(n)
			}
			slot = int(slots[biome]) - 1
		} else {
			slot = slices.Index(palette[:n], biome)
			if slot < 0 {
				slot = n
				palette[n] = biome
				n++
			}
		}
		index[cell] = uint16(slot)
	}
	return n
}

// putPacked writes count entries of entryBits bits into longs from the low bits
// up. Entries never cross a long boundary (the top bits pad), and the array
// length is not sent; the client derives it from the bits per entry.
func putPacked(b *protocol.Buffer, entryBits, count int, at func(int) uint64) {
	perLong := 64 / entryBits
	for i := 0; i < count; i += perLong {
		var packed uint64
		for j := 0; j < perLong && i+j < count; j++ {
			packed |= at(i+j) << (j * entryBits)
		}
		b.PutI64(int64(packed))
	}
}

// structuresTag returns the chunk's structures tag in carriedRoot, creating it.
func (c *Chunk) structuresTag(key string) nbt.Compound {
	if c.carriedRoot == nil {
		c.carriedRoot = nbt.Compound{}
	}
	structures, ok := nbt.Get[nbt.Compound](c.carriedRoot, "structures")
	if !ok {
		structures = nbt.Compound{}
		c.carriedRoot["structures"] = structures
	}
	tag, ok := nbt.Get[nbt.Compound](structures, key)
	if !ok {
		tag = nbt.Compound{}
		structures[key] = tag
	}
	return tag
}

// setStructureStart records a structure start in the chunk's saved form.
func (c *Chunk) setStructureStart(id string, start nbt.Compound) {
	c.structuresTag("starts")[id] = start
}

// setLiveStructureStart records a start that is saved as it stands when the
// chunk is encoded, not as it was when generated.
func (c *Chunk) setLiveStructureStart(id string, start func() nbt.Compound) {
	c.setStructureStart(id, start())
	if c.liveStarts == nil {
		c.liveStarts = map[string]func() nbt.Compound{}
	}
	c.liveStarts[id] = start
}

// savedRoot returns carriedRoot with the live starts as they stand now.
func (c *Chunk) savedRoot() nbt.Compound {
	if len(c.liveStarts) == 0 {
		return c.carriedRoot
	}
	structureView, _ := nbt.Get[nbt.Compound](c.carriedRoot, "structures")
	structures := maps.Clone(structureView)
	startsView, _ := nbt.Get[nbt.Compound](structures, "starts")
	starts := maps.Clone(startsView)
	for id, start := range c.liveStarts {
		starts[id] = start()
	}
	structures["starts"] = starts
	root := maps.Clone(c.carriedRoot)
	root["structures"] = structures
	return root
}

// setStructureReferences records the chunks whose starts reach into this one,
// as packed positions.
func (c *Chunk) setStructureReferences(id string, refs []int64) {
	c.structuresTag("References")[id] = nbt.LongArray(refs)
}

// GeneratedTicks returns the generator's scheduled tick positions in world coordinates and placement order.
func (c *Chunk) GeneratedTicks() [][3]int32 {
	var out [][3]int32
	for _, p := range c.pendingGenerated {
		out = append(out, [3]int32{c.x*16 + p[0], p[1], c.z*16 + p[2]})
	}
	return out
}

// BlockEntitiesNBT returns the block entities a save would write, in save order.
func (c *Chunk) BlockEntitiesNBT() []nbt.Compound {
	var out []nbt.Compound
	all := append(encodeBlockEntities(c.blockEntities()), c.encodePistonRides()...)
	all = append(all, c.carriedBlockEntities.Items...)
	for _, tag := range all {
		if compound, ok := nbt.CompoundView(tag); ok {
			out = append(out, compound)
		}
	}
	return out
}

// StructureStarts lists the structures whose start is in this chunk, by id.
func (c *Chunk) StructureStarts() []string {
	structures, _ := nbt.Get[nbt.Compound](c.carriedRoot, "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	var out []string
	for id, tag := range starts {
		if start, ok := nbt.CompoundView(tag); ok {
			if x, _ := nbt.IntField(start, "ChunkX"); x == c.x {
				if z, _ := nbt.IntField(start, "ChunkZ"); z == c.z {
					out = append(out, id)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}
