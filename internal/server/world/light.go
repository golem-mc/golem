package world

import (
	"math"
	"math/bits"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// lightRadius is how far a change can affect light, and so the recompute
// radius: both kinds decay at least 1 per block from at most 15.
const lightRadius = 15

// Light layer indices for the BFS queue.
const (
	skyLayer   = 0
	blockLayer = 1
)

// spread uses registry's light direction indices (the Direction ordinals the
// per-face occlusion test is keyed by), the same ordinals as redstone.go's
// dirDown..dirEast.

// lightSectionOf maps a world Y to its light section index: 0 below the world,
// 1..sectionCount inside it, sectionCount+1 above. Floor division keeps a Y
// below the world below index 0 rather than folding it in.
func lightSectionOf(y int32) int {
	return int(floorDiv(y-worldMinY, sectionSize)) + 1
}

// lightLocalY is a world Y's offset within its light section, 0..sectionSize-1.
func lightLocalY(y int32) int32 {
	return (y - worldMinY) - int32(lightSectionOf(y)-1)*sectionSize
}

// lightNibbleIndex returns the byte holding a section-local nibble and whether
// it is the high one.
func lightNibbleIndex(lx, ly, lz int32) (i int, high bool) {
	i = blockIndex(lx, ly, lz)
	return i / 2, i&1 != 0
}

// lightNibble reads a section-local nibble.
func lightNibble(s *[lightArrayLen]byte, lx, ly, lz int32) uint8 {
	i, high := lightNibbleIndex(lx, ly, lz)
	return lightNibbleAt(s, i, high)
}

func lightNibbleAt(s *[lightArrayLen]byte, i int, high bool) uint8 {
	if s == nil {
		return 0
	}
	if high {
		return s[i] >> 4
	}
	return s[i] & 0x0F
}

// lightGet returns the section-local light value. A nil array is all zero, and
// a Y outside the light sections reads 0; the World accessors apply the
// dimension's rule around it.
func (c *Chunk) lightGet(layer *[lightSections]*[lightArrayLen]byte, lx, y, lz int32) uint8 {
	si := lightSectionOf(y)
	if si < 0 || si >= lightSections {
		return 0
	}
	s := layer[si]
	if s == nil {
		return 0
	}
	return lightNibble(s, lx, lightLocalY(y), lz)
}

// lightSet writes a section-local light value of the layer (skyLayer or
// blockLayer), through writableLight; a Y outside the light sections is
// ignored.
func (c *Chunk) lightSet(layer int, lx, y, lz int32, v uint8) {
	si := lightSectionOf(y)
	if si < 0 || si >= lightSections {
		return
	}
	if v == 0 && c.lightLayer(layer)[si] == nil {
		return
	}
	s := c.writableLight(layer, si)
	i, high := lightNibbleIndex(lx, lightLocalY(y), lz)
	if high {
		s[i] = (s[i] & 0x0F) | ((v & 0x0F) << 4)
	} else {
		s[i] = (s[i] & 0xF0) | (v & 0x0F)
	}
}

// lightLayer is the chunk's arrays of the layer, skyLayer or blockLayer.
func (c *Chunk) lightLayer(layer int) *[lightSections]*[lightArrayLen]byte {
	if layer == skyLayer {
		return &c.skyLight
	}
	return &c.blockLight
}

// writableLight is section si of the chunk's layer, ready to write in place.
func (c *Chunk) writableLight(layer, si int) *[lightArrayLen]byte {
	return writableLight(c.lightLayer(layer), &c.lightBorrowed[layer], si)
}

// allLightSections is a lightBorrowed mask with every light section set.
const allLightSections uint32 = 1<<lightSections - 1

// lightArrays recycles light arrays: a shadow's copies when its relight is
// dropped, and the arrays writableLight hands out.
var lightArrays = sync.Pool{New: func() any { return new([lightArrayLen]byte) }}

// newLightArray is a light array holding from's nibbles, for a section about
// to be written.
func newLightArray(from *[lightArrayLen]byte) *[lightArrayLen]byte {
	a := lightArrays.Get().(*[lightArrayLen]byte)
	*a = *from
	return a
}

// writableLight returns section si of arrs to write in place: a fresh dark
// array where there is none, a private copy where borrowed marks the array as
// shared (lightBorrowed), which a write must never reach.
func writableLight(arrs *[lightSections]*[lightArrayLen]byte, borrowed *uint32, si int) *[lightArrayLen]byte {
	s, bit := arrs[si], uint32(1)<<si
	if s != nil && *borrowed&bit == 0 {
		return s
	}
	if s == nil {
		s = &darkSection
	}
	s = newLightArray(s)
	arrs[si] = s
	*borrowed &^= bit
	return s
}

// skyLightBelow reads the stored cell, or the bottom row of the nearest
// stored section above when the section is missing, or 15 past top.
// vanilla: SkyLightSectionStorage.getLightValue
func (c *Chunk) skyLightBelow(lx, y, lz int32, top int) uint8 {
	si, ly := lightSectionOf(y), lightLocalY(y)
	if si < 0 {
		si, ly = 0, 0
	}
	for ; si < top; si++ {
		if s := c.skyLight[si]; s != nil {
			return lightNibble(s, lx, ly, lz)
		}
		ly = 0
	}
	return 15
}

// lightTopSection is the light section just above the dimension, at or above
// which sky light reads 15 (the overworld's is over y=319).
func (w *World) lightTopSection() int {
	return lightSectionOf(w.dim.minY + w.dim.height)
}

// SkyLightAt is the sky light at the coordinates: 0 in an unloaded chunk or a
// dimension without sky light (no engine fills it), 15 above the dimension, and
// below it the stored cell or the bottom row of the nearest stored section above.
// vanilla: LevelLightEngine.getRawBrightness, SkyLightSectionStorage.getLightValue
func (w *World) SkyLightAt(x, y, z int32) uint8 {
	if !w.dim.skyLight {
		return 0
	}
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return 0
	}
	c := loaded.chunk
	if y >= w.dim.minY+w.dim.height {
		return 15
	}
	if y < w.dim.minY {
		return c.skyLightBelow(x&15, y, z&15, w.lightTopSection())
	}
	return c.lightGet(&c.skyLight, x&15, y, z&15)
}

// BlockLightAt is the block light at the coordinates: 0 in an unloaded chunk or
// outside the dimension, where vanilla stores no section.
// vanilla: LayerLightSectionStorage.getStoredLevel
func (w *World) BlockLightAt(x, y, z int32) uint8 {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return 0
	}
	if y < w.dim.minY || y >= w.dim.minY+w.dim.height {
		return 0
	}
	return loaded.chunk.lightGet(&loaded.chunk.blockLight, x&15, y, z&15)
}

// lightArrayEmpty reports an all-zero (or nil) light array.
func lightArrayEmpty(a *[lightArrayLen]byte) bool {
	return a == nil || *a == darkSection
}

// lightArrayFull reports an array of all 15s, the uniformly lit sky section the
// wire omits from both masks so the client keeps its full default.
func lightArrayFull(a *[lightArrayLen]byte) bool {
	return *a == fullSection
}

// encodeLightForm writes the wire light data: four BitSets (sky, block, empty
// sky, empty block) then the 2048-byte sky and block arrays. Uniformly dark
// sections go in the empty masks; omitFullSky also omits uniformly lit sky
// sections from both sky masks, which only a whole-chunk send may do since the
// client starts every section full.
// vanilla: ClientboundLightUpdatePacketData
func (c *Chunk) encodeLightForm(omitFullSky bool, f lightFilter) []byte {
	var m lightMasks
	for i := range lightSections {
		sky := c.skyLight[i]
		switch {
		case lightArrayEmpty(sky):
			m.emptySky |= 1 << i
		case omitFullSky && lightArrayFull(sky):
			// In neither sky mask: the client's default is full.
		default:
			m.sky |= 1 << i
		}
		if lightArrayEmpty(c.blockLight[i]) {
			m.emptyBlock |= 1 << i
		} else {
			m.block |= 1 << i
		}
	}
	return m.filter(f).encode(c.skyLight[:], c.blockLight[:])
}

// lightFilter keeps the light sections to send: bit i of sky or block is light
// section i of that layer.
type lightFilter struct{ sky, block uint64 }

// allLight keeps every section.
var allLight = lightFilter{^uint64(0), ^uint64(0)}

// filter ANDs each layer's mask and empty mask with that layer's filter.
func (m lightMasks) filter(f lightFilter) lightMasks {
	return lightMasks{m.sky & f.sky, m.block & f.block, m.emptySky & f.sky, m.emptyBlock & f.block}
}

// lightMasks is the wire light data's four BitSets, bit i for light section i.
type lightMasks struct{ sky, block, emptySky, emptyBlock uint64 }

// encode writes the masks, then the arrays of the sections in the sky and the
// block mask, sky[i] and block[i] being section i's.
func (m lightMasks) encode(sky, block []*[lightArrayLen]byte) []byte {
	var b protocol.Buffer
	// The masks and counts fit in 64 bytes; each array has a 2-byte varint
	// length.
	b.Grow(64 + (bits.OnesCount64(m.sky)+bits.OnesCount64(m.block))*(2+lightArrayLen))
	b.PutBitSet([]uint64{m.sky})
	b.PutBitSet([]uint64{m.block})
	b.PutBitSet([]uint64{m.emptySky})
	b.PutBitSet([]uint64{m.emptyBlock})
	putLightArrays(&b, m.sky, sky)
	putLightArrays(&b, m.block, block)
	return b.Bytes()
}

// putLightArrays writes the count, then the array of each section in mask.
func putLightArrays(b *protocol.Buffer, mask uint64, arrays []*[lightArrayLen]byte) {
	b.PutVarInt(int32(bits.OnesCount64(mask)))
	for i, arr := range arrays {
		if mask&(1<<i) != 0 {
			b.PutBytes(arr[:])
		}
	}
}

// encodeLight is the light as Update Light and the disk cache carry it: every
// non-empty section, uniformly lit ones included, so an update corrects a
// section that became full instead of leaving it stale.
func (c *Chunk) encodeLight() []byte { return c.encodeLightForm(false, allLight) }

// encodeChunkLight is a Chunk Data packet's light: uniformly lit sky sections
// are omitted, safe because of the client's full default.
func (c *Chunk) encodeChunkLight() []byte { return c.encodeLightForm(true, allLight) }

// decodeLight parses encodeLight's format back into the chunk's arrays, so
// cached light loads from disk without a recompute.
func (c *Chunk) decodeLight(data []byte) bool {
	r := protocol.NewReader(data)
	skyMask := r.ReadBitSet()
	blockMask := r.ReadBitSet()
	// The empty masks name the all-zero sections, which is what absent sections
	// already read as (nil here), so both are skipped.
	r.ReadBitSet()
	r.ReadBitSet()
	if r.Err() != nil {
		return false
	}

	// Read sky arrays: one per bit set in skyMask, in ascending section order.
	nSky := r.ReadVarInt()
	for i := range lightSections {
		if skyMask&(1<<i) == 0 {
			continue
		}
		if nSky <= 0 {
			return false
		}
		nSky--
		raw := r.ReadBytes(lightArrayLen)
		if len(raw) != lightArrayLen {
			return false
		}
		c.loadLightArray(skyLayer, i, (*[lightArrayLen]byte)(raw))
	}

	// Read block arrays: one per bit set in blockMask.
	nBlock := r.ReadVarInt()
	for i := range lightSections {
		if blockMask&(1<<i) == 0 {
			continue
		}
		if nBlock <= 0 {
			return false
		}
		nBlock--
		raw := r.ReadBytes(lightArrayLen)
		if len(raw) != lightArrayLen {
			return false
		}
		c.loadLightArray(blockLayer, i, (*[lightArrayLen]byte)(raw))
	}

	return r.Err() == nil
}

// loadLightArray shares uniform cached sections without losing the distinction
// between a stored dark sky section and an absent one (skyLightBelow).
func (c *Chunk) loadLightArray(layer, si int, a *[lightArrayLen]byte) {
	var shared *[lightArrayLen]byte
	switch *a {
	case darkSection:
		shared = &darkSection
	case fullSection:
		shared = &fullSection
	default:
		c.lightLayer(layer)[si] = newLightArray(a)
		c.lightBorrowed[layer] &^= 1 << si
		return
	}
	c.lightLayer(layer)[si] = shared
	c.lightBorrowed[layer] |= 1 << si
}

// LightPacket builds an Update Light (0x30) payload for the chunk, nil if not
// loaded.
func (w *World) LightPacket(pos ChunkPos) []byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return nil
	}
	var b protocol.Buffer
	b.PutVarInt(pos.X)
	b.PutVarInt(pos.Z)
	b.PutRaw(loaded.chunk.lightForm())
	return b.Bytes()
}

// hasLightForms reports whether the chunk has encoded light, current or stale.
func (c *Chunk) hasLightForms() bool {
	c.lightFormsMu.Lock()
	defer c.lightFormsMu.Unlock()
	return c.lightFormsInit || c.lightEncoded != nil
}

// invalidateLightForms marks both encoded forms out of date. The caller holds
// w.mu for writing.
func (c *Chunk) invalidateLightForms() {
	c.lightFormsInit = true
	c.lightEncoded, c.chunkLightEncoded = nil, nil
}

// lightForm returns the Update Light form of the chunk's light, building it
// if out of date. The caller holds w.mu, read or write, so the arrays are not
// written meanwhile.
func (c *Chunk) lightForm() []byte {
	return c.encodedLightForm(&c.lightEncoded, c.encodeLight)
}

// chunkLightForm is lightForm for the Chunk Data form.
func (c *Chunk) chunkLightForm() []byte {
	return c.encodedLightForm(&c.chunkLightEncoded, c.encodeChunkLight)
}

func (c *Chunk) encodedLightForm(slot *[]byte, encode func() []byte) []byte {
	c.lightFormsMu.Lock()
	defer c.lightFormsMu.Unlock()
	if *slot != nil || !c.lightFormsInit {
		return *slot
	}
	form := encode()
	if c.lightFormsRetained {
		*slot = form
	}
	return form
}

// --- Engine ---

// floorDiv divides two integers rounding towards negative infinity.
func floorDiv(a, b int32) int32 {
	q := a / b
	if (a^b) < 0 && a%b != 0 {
		q--
	}
	return q
}

// lightCtx caches chunks in a 4x4 direct-mapped table to skip map lookups:
// light spreads at most 15 blocks, so any 4x4 chunk neighborhood (64x64 blocks)
// fits without collisions, including unloaded nil neighbors.
// vanilla: LightEngine.lastChunkPos / lastChunk
type lightCtx struct {
	w *World
	// cache is indexed by (cx&3)<<2 | (cz&3); cacheValid tracks populated slots
	// so unloaded (nil) chunks are cached too.
	cache [16]struct {
		c      *Chunk
		cx, cz int32
	}
	cacheValid uint16
	// alone, when set, is the only chunk the engine sees: one not yet in the
	// world, lit without w.mu with everything around unloaded.
	alone *Chunk
	// touched, when non-nil, collects each chunk setLight raised a cell in,
	// once, so only those are re-encoded.
	touched     *[]*Chunk
	lastTouched *Chunk // the chunk last found in touched
	// extra, when set, is a chunk not yet in the map that the engine sees at
	// its position: one being joined before its insert.
	extra *Chunk
	// cow, when non-nil, holds the light reads and writes instead of the
	// chunks' arrays, so a run under the read lock leaves the world untouched;
	// lastC and lastO cache the latest lookup.
	cow   map[*Chunk]*lightOverlay
	lastC *Chunk
	lastO *lightOverlay
}

// lightOverlay is a chunk's light as a copy-on-write run sees it: its section
// pointers, each written section replaced by a private copy. Both are indexed
// by skyLayer, blockLayer; borrowed is Chunk.lightBorrowed's mask.
type lightOverlay struct {
	layers   [2][lightSections]*[lightArrayLen]byte
	borrowed [2]uint32
}

// layer is the light array set of c the engine reads and writes for layer.
func (ctx *lightCtx) layer(c *Chunk, layer int) *[lightSections]*[lightArrayLen]byte {
	if ctx.cow == nil {
		return c.lightLayer(layer)
	}
	return &ctx.overlay(c).layers[layer]
}

// writable is section si of c's layer as ctx sees it, ready to write in place.
func (ctx *lightCtx) writable(c *Chunk, layer, si int) *[lightArrayLen]byte {
	if ctx.cow == nil {
		return c.writableLight(layer, si)
	}
	o := ctx.overlay(c)
	return writableLight(&o.layers[layer], &o.borrowed[layer], si)
}

func (ctx *lightCtx) overlay(c *Chunk) *lightOverlay {
	if ctx.lastC == c {
		return ctx.lastO
	}
	o := ctx.cow[c]
	if o == nil {
		o = &lightOverlay{
			layers:   [2][lightSections]*[lightArrayLen]byte{c.skyLight, c.blockLight},
			borrowed: [2]uint32{allLightSections, allLightSections},
		}
		ctx.cow[c] = o
	}
	ctx.lastC, ctx.lastO = c, o
	return o
}

// chunkAt is the chunk at (cx, cz), nil if not loaded.
func (ctx *lightCtx) chunkAt(cx, cz int32) *Chunk {
	if ctx.extra != nil && cx == ctx.extra.x && cz == ctx.extra.z {
		return ctx.extra
	}
	if ctx.alone != nil {
		if cx == ctx.alone.x && cz == ctx.alone.z {
			return ctx.alone
		}
		return nil
	}
	loaded, ok := ctx.w.chunks[ChunkPos{cx, cz}]
	if !ok {
		return nil
	}
	return loaded.chunk
}

func (ctx *lightCtx) resolve(x, z int32) (*Chunk, bool) {
	cx, cz := x>>4, z>>4 // floorDiv by sectionSize, which is 16
	slot := uint((cx&3)<<2 | (cz & 3))
	e := &ctx.cache[slot]
	if ctx.cacheValid&(1<<slot) != 0 && e.cx == cx && e.cz == cz {
		return e.c, e.c != nil
	}
	c := ctx.chunkAt(cx, cz)
	e.c, e.cx, e.cz = c, cx, cz
	ctx.cacheValid |= 1 << slot
	return c, c != nil
}

func (ctx *lightCtx) blockAt(x, y, z int32) (uint16, bool) {
	c, ok := ctx.resolve(x, z)
	if !ok {
		return airState, false
	}
	return c.sharedBlockAt(x&15, y, z&15), true
}

func (ctx *lightCtx) getLight(layer int, x, y, z int32) uint8 {
	c, ok := ctx.resolve(x, z)
	if !ok {
		return 0
	}
	return c.lightGet(ctx.layer(c, layer), x&15, y, z&15)
}

// setLight raises the cell to v (above 0) if it holds less, reporting whether
// it did.
func (ctx *lightCtx) setLight(layer int, x, y, z int32, v uint8) bool {
	c, ok := ctx.resolve(x, z)
	if !ok {
		return false
	}
	si := lightSectionOf(y)
	if si < 0 || si >= lightSections {
		return false
	}
	i, high := lightNibbleIndex(x&15, lightLocalY(y), z&15)
	if s := ctx.layer(c, layer)[si]; s != nil && lightNibbleAt(s, i, high) >= v {
		return false
	}
	ctx.writeLightNibble(c, layer, si, i, high, v)
	return true
}

func (ctx *lightCtx) writeLightNibble(c *Chunk, layer, si, i int, high bool, v uint8) {
	s := ctx.writable(c, layer, si)
	if high {
		s[i] = s[i]&0x0F | v<<4
	} else {
		s[i] = s[i]&0xF0 | v&0x0F
	}
	ctx.touch(c)
}

// touch records that a cell of c was written.
func (ctx *lightCtx) touch(c *Chunk) {
	if ctx.touched != nil && c != ctx.lastTouched {
		if !slices.Contains(*ctx.touched, c) {
			*ctx.touched = append(*ctx.touched, c)
		}
		ctx.lastTouched = c
	}
}

// spread is the light passing from a cell holding v and fromState, along dir,
// into a neighbour holding nbState: less the neighbour's dampening (at least
// 1), or nothing when the two shapes cover the shared face (in vanilla's rules
// only between shape-occluding states like slabs and stairs). Sky light at 15
// needs no special case: such cells are column sources, and the cell below one
// is a source itself or sealed by the face test the column fill used.
func spread(v uint8, dir int, fromState, nbState uint16) uint8 {
	if registry.LightFacesSeal(fromState, dir, nbState) {
		return 0
	}
	damp := max(registry.LightDampening(nbState), 1)
	if int(v) <= damp {
		return 0
	}
	return v - uint8(damp)
}

// lightEntry is a pending light propagation, packed into 16 bytes.
type lightEntry struct {
	x, y, z      int32
	v            uint8
	layer        uint8
	statePlusOne uint16 // zero if unknown or the state ID wrapped when offset
}

type lightQueue struct {
	entries []lightEntry
	head    int
}

// lightQueues recycles BFS queues, which reach hundreds of thousands of entries
// for a chunk's sky light.
var lightQueues = sync.Pool{New: func() any { return new(lightQueue) }}

// release empties the queue and returns it to lightQueues.
func (q *lightQueue) release() {
	q.entries, q.head = q.entries[:0], 0
	lightQueues.Put(q)
}

func (q *lightQueue) push(x, y, z int32, v uint8, layer int) {
	q.pushWithState(x, y, z, v, layer, 0)
}

// pushWithState appends an entry; a full array first reuses the consumed prefix when it
// is at least as large as the unread suffix, so the copy is amortised.
func (q *lightQueue) pushWithState(x, y, z int32, v uint8, layer int, statePlusOne uint16) {
	if len(q.entries) == cap(q.entries) && q.head > 0 && q.head >= len(q.entries)-q.head {
		n := copy(q.entries, q.entries[q.head:])
		q.entries, q.head = q.entries[:n], 0
	}
	q.entries = append(q.entries, lightEntry{x, y, z, v, uint8(layer), statePlusOne})
}

func (q *lightQueue) pop() (lightEntry, bool) {
	if q.head >= len(q.entries) {
		return lightEntry{}, false
	}
	e := q.entries[q.head]
	q.head++
	return e, true
}

// runLightBFS spreads each queued cell's light to its six neighbours, raising a
// neighbour only above what it holds; unloaded chunks stop it.
func (ctx *lightCtx) runLightBFS(q *lightQueue) {
	neighbors := [6]struct{ dx, dy, dz, dir int32 }{
		{0, -1, 0, dirDown},
		{0, 1, 0, dirUp},
		{0, 0, -1, dirNorth},
		{0, 0, 1, dirSouth},
		{-1, 0, 0, dirWest},
		{1, 0, 0, dirEast},
	}
	for {
		e, ok := q.pop()
		if !ok {
			return
		}
		fromState, _ := ctx.blockAt(e.x, e.y, e.z)
		for _, n := range neighbors {
			nx, ny, nz := e.x+n.dx, e.y+n.dy, e.z+n.dz
			if ny < worldMinY || ny >= worldMinY+worldHeight {
				continue
			}
			nbState, loaded := ctx.blockAt(nx, ny, nz)
			if !loaded {
				continue
			}
			cand := spread(e.v, int(n.dir), fromState, nbState)
			if cand == 0 {
				continue
			}
			if ctx.setLight(int(e.layer), nx, ny, nz, cand) {
				q.push(nx, ny, nz, cand, int(e.layer))
			}
		}
	}
}

// wipeLightBox clears sky and block light in the box for loaded chunks: whole
// sections inside are nilled, partial ones cleared cell by cell.
func (w *World) wipeLightBox(minX, minZ, maxX, maxZ int32) {
	minCx, minCz := floorDiv(minX, sectionSize), floorDiv(minZ, sectionSize)
	maxCx, maxCz := floorDiv(maxX, sectionSize), floorDiv(maxZ, sectionSize)
	for cx := minCx; cx <= maxCx; cx++ {
		for cz := minCz; cz <= maxCz; cz++ {
			loaded, ok := w.chunks[ChunkPos{cx, cz}]
			if !ok {
				continue
			}
			c := loaded.chunk
			cminX, cmaxX := cx*16, cx*16+15
			cminZ, cmaxZ := cz*16, cz*16+15
			fullX := cminX >= minX && cmaxX <= maxX
			fullZ := cminZ >= minZ && cmaxZ <= maxZ
			lx0, lx1 := max(minX, cminX)-cminX, min(maxX, cmaxX)-cminX
			lz0, lz1 := max(minZ, cminZ)-cminZ, min(maxZ, cmaxZ)-cminZ
			for sy := range lightSections {
				if fullX && fullZ {
					c.skyLight[sy] = nil
					c.blockLight[sy] = nil
					continue
				}
				// The light sections below and above the world hold nothing to
				// clear.
				if sy == 0 || sy == lightSections-1 {
					continue
				}
				for layer := range 2 {
					if c.lightLayer(layer)[sy] != nil {
						clearNibbleBox(c.writableLight(layer, sy), lx0, lx1, lz0, lz1)
					}
				}
			}
		}
	}
}

// clearNibbleBox zeroes every row of a section with local x in [lx0, lx1] and z
// in [lz0, lz1], at every y.
func clearNibbleBox(s *[lightArrayLen]byte, lx0, lx1, lz0, lz1 int32) {
	setNibbleBox(s, lx0, lx1, lz0, lz1, 0)
}

// setNibbleBox sets to v every row with local x in [lx0, lx1] and z in [lz0,
// lz1], at every y; a row's x run is contiguous, two cells a byte.
func setNibbleBox(s *[lightArrayLen]byte, lx0, lx1, lz0, lz1 int32, v uint8) {
	both := v&0x0F | v<<4
	for ly := range int32(sectionSize) {
		for lz := lz0; lz <= lz1; lz++ {
			half := blockIndex(0, ly, lz) / 2
			x0, x1 := lx0, lx1
			if x0&1 == 1 { // the run starts on a high nibble
				s[half+int(x0)/2] = s[half+int(x0)/2]&0x0F | v<<4
				x0++
			}
			if x1&1 == 0 && x1 >= x0 { // the run ends on a low nibble
				s[half+int(x1)/2] = s[half+int(x1)/2]&0xF0 | v&0x0F
				x1--
			}
			for i := half + int(x0)/2; i <= half+int(x1)/2 && x1 > x0; i++ {
				s[i] = both
			}
		}
	}
}

// fillSkyColumns lights each column in the box top-down, as
// ChunkSkyLightSources does. Starting from the virtual air above the top, each
// cell is a 15 source until an edge into it is occluded: the cell below dampens
// (a full block, leaves, water) or the two shapes cover the shared face (a
// bottom slab's underside). Below that edge the BFS lights from the column's
// sides and, for a dampening block, from the source above.
//
// Every cell left below 15 seeds the BFS, but a 15 cell seeds only where it can
// light something: sideways an in-box neighbour's run of 15 or a loaded outside
// cell under 14, and downward only the column's lowest source. Otherwise the
// open sky, most of a chunk, would go through the queue to light nothing.
func (ctx *lightCtx) fillSkyColumns(minX, minZ, maxX, maxZ int32, q *lightQueue) {
	const worldTop = worldMinY + worldHeight
	// floors holds per box column the lowest y left at 15 (worldTop with no
	// sources), or noColumn where the chunk is not loaded.
	const noColumn = worldMinY - 1
	width, depth := maxX-minX+1, maxZ-minZ+1
	floors := make([]int32, width*depth)
	for i := range floors {
		floors[i] = noColumn
	}

	minCx, minCz := floorDiv(minX, sectionSize), floorDiv(minZ, sectionSize)
	maxCx, maxCz := floorDiv(maxX, sectionSize), floorDiv(maxZ, sectionSize)
	for cx := minCx; cx <= maxCx; cx++ {
		for cz := minCz; cz <= maxCz; cz++ {
			c := ctx.chunkAt(cx, cz)
			if c == nil {
				continue
			}
			lx0 := max(cx*16, minX) - cx*16
			lx1 := min(cx*16+15, maxX) - cx*16
			lz0 := max(cz*16, minZ) - cz*16
			lz1 := min(cz*16+15, maxZ) - cz*16
			// Sections above the highest non-empty one are open air in every
			// column: the scan starts below them, and a whole-chunk box lights
			// them as arrays of 15.
			startY := int32(worldTop)
			for startY > worldMinY && c.sections[(startY-worldMinY)/sectionSize-1].isUniform(airState) {
				startY -= sectionSize
			}
			whole := lx0 == 0 && lx1 == 15 && lz0 == 0 && lz1 == 15
			for y := startY; y < worldTop; y += sectionSize {
				si := lightSectionOf(y)
				if whole {
					c.skyLight[si] = &fullSection
					c.lightBorrowed[skyLayer] |= 1 << si
					continue
				}
				setNibbleBox(c.writableLight(skyLayer, si), lx0, lx1, lz0, lz1, 15)
			}
			for lx := lx0; lx <= lx1; lx++ {
				for lz := lz0; lz <= lz1; lz++ {
					wx, wz := cx*16+lx, cz*16+lz
					floor := startY
					var topState uint16 = airState // the virtual air above the world
					for y := startY - 1; y >= worldMinY; y-- {
						state := c.sharedBlockAt(lx, y, lz)
						// Air under air, the open sky, needs no table lookups.
						if (state != airState || topState != airState) &&
							(registry.LightDampening(state) != 0 || registry.LightFacesSeal(topState, dirDown, state)) {
							break // the edge into this cell is occluded: it and everything below go through the BFS
						}
						c.lightSet(skyLayer, lx, y, lz, 15)
						floor = y
						topState = state
					}
					floors[(wx-minX)*depth+(wz-minZ)] = floor
				}
			}
			// The section above the world is always full sky light (15).
			si := lightSections - 1
			if c.skyLight[si] == nil {
				c.skyLight[si] = &fullSection
				c.lightBorrowed[skyLayer] |= 1 << si
			}
		}
	}

	// The 15 cells with a neighbour to light: beside an in-box neighbour's run
	// of 15 or a loaded outside cell under 14, plus each column's lowest
	// source, whose below-neighbour is the first cell the column did not light
	// itself.
	sides := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	// Count by offset into the box, so a box ending at math.MaxInt32 stops
	// instead of wrapping.
	for i := range width {
		x := minX + i
		for j := range depth {
			z := minZ + j
			floor := floors[i*depth+j]
			if floor == noColumn {
				continue
			}
			// below is the highest of the in-box neighbours' floors, under
			// which one may hold less than 15; a neighbour without sources sits
			// at worldTop and wants light at every height.
			below := floor
			// The outside neighbours' sky arrays, looked up once per column
			// rather than per cell.
			var outside [4]struct {
				c      *Chunk
				arr    *[lightSections]*[lightArrayLen]byte
				lx, lz int32
			}
			n := 0
			for _, d := range sides {
				ni, nj := i+d[0], j+d[1]
				if ni < 0 || ni >= width || nj < 0 || nj >= depth {
					nx, okx := stepped(x, d[0])
					nz, okz := stepped(z, d[1])
					if !okx || !okz {
						continue
					}
					if c, ok := ctx.resolve(nx, nz); ok {
						outside[n].c, outside[n].arr = c, ctx.layer(c, skyLayer)
						outside[n].lx, outside[n].lz = nx&15, nz&15
						n++
					}
					continue
				}
				if f := floors[ni*depth+nj]; f != noColumn {
					below = max(below, f)
				}
			}
			// With no loaded neighbour outside the box, the open sky above the
			// tallest floor seeds nothing, so the walk stops there.
			top := int32(worldTop)
			if n == 0 {
				top = min(top, max(floor+1, below))
			}
			for y := floor; y < top; y++ {
				seed := y == floor || y < below
				if !seed {
					for i := 0; !seed && i < n; i++ {
						o := &outside[i]
						seed = o.c.lightGet(o.arr, o.lx, y, o.lz) < 14
					}
				}
				if seed {
					q.push(x, y, z, 15, skyLayer)
				}
			}
		}
	}
}

// scanEmitters seeds the box's block light emitters, a chunk at a time,
// skipping sections with no emitter (nearly all).
// vanilla: ChunkAccess.findBlockLightSources
func (ctx *lightCtx) scanEmitters(minX, minZ, maxX, maxZ int32, q *lightQueue) {
	minCx, minCz := floorDiv(minX, sectionSize), floorDiv(minZ, sectionSize)
	maxCx, maxCz := floorDiv(maxX, sectionSize), floorDiv(maxZ, sectionSize)
	for cx := minCx; cx <= maxCx; cx++ {
		for cz := minCz; cz <= maxCz; cz++ {
			c := ctx.chunkAt(cx, cz)
			if c == nil {
				continue
			}
			var emitting [sectionCount]int
			n := 0
			for si := range c.sections {
				if c.sections[si].emitting > 0 {
					emitting[n] = si
					n++
				}
			}
			if n == 0 {
				continue
			}
			lx0 := max(cx*16, minX) - cx*16
			lx1 := min(cx*16+15, maxX) - cx*16
			lz0 := max(cz*16, minZ) - cz*16
			lz1 := min(cz*16+15, maxZ) - cz*16
			for lx := lx0; lx <= lx1; lx++ {
				for lz := lz0; lz <= lz1; lz++ {
					wx, wz := cx*16+lx, cz*16+lz
					for _, si := range emitting[:n] {
						y0 := worldMinY + int32(si)*sectionSize
						for y := y0; y < y0+sectionSize; y++ {
							state := c.sharedBlockAt(lx, y, lz)
							e := uint8(registry.LightEmission(state))
							if e == 0 {
								continue
							}
							if c.lightGet(&c.blockLight, lx, y, lz) < e {
								c.lightSet(blockLayer, lx, y, lz, e)
								q.push(wx, y, wz, e, blockLayer)
							}
						}
					}
				}
			}
		}
	}
}

// seedShell queues the cells just outside the box that would raise the box cell
// beside them. The outside was lit before and is left as is, so inward is the
// only way a shell cell has anything new to give. It walks one column at a
// time, looking up each column's two chunks once.
func (ctx *lightCtx) seedShell(minX, minZ, maxX, maxZ int32, q *lightQueue) {
	shades := lightShades{}
	west, east, north, south := boxSides(minX, minZ, maxX, maxZ)
	for i := range maxZ - minZ + 1 {
		z := minZ + i
		if west {
			ctx.seedAcross(minX-1, z, minX, z, dirEast, q, shades)
		}
		if east {
			ctx.seedAcross(maxX+1, z, maxX, z, dirWest, q, shades)
		}
	}
	for i := range maxX - minX + 1 {
		x := minX + i
		if north {
			ctx.seedAcross(x, minZ-1, x, minZ, dirSouth, q, shades)
		}
		if south {
			ctx.seedAcross(x, maxZ+1, x, maxZ, dirNorth, q, shades)
		}
	}
}

// boxSides reports which sides have a column beyond them: none past the int32
// range, where minX-1 or maxX+1 would wrap.
func boxSides(minX, minZ, maxX, maxZ int32) (west, east, north, south bool) {
	return minX > math.MinInt32, maxX < math.MaxInt32, minZ > math.MinInt32, maxZ < math.MaxInt32
}

// stepped is v+d for d in -1..1, false where it would wrap past int32.
func stepped(v, d int32) (int32, bool) {
	s := v + d
	return s, (s < v) == (d < 0)
}

// addClamped is v+d clamped to int32, so a box grown by the light radius at the
// range's edge stops instead of wrapping.
func addClamped(v, d int32) int32 {
	return int32(min(max(int64(v)+int64(d), math.MinInt32), math.MaxInt32))
}

// lightShades caches per chunk whether each section array is uniformly dark
// (nil included) or 15, for one seeding pass that writes no light. Keyed by
// chunk so a border column costs two lookups per section, not three.
type lightShades map[*Chunk]*chunkShades

// chunkShades are one chunk's shades, by light section.
type chunkShades struct {
	sky, block [lightSections]uint8
}

const (
	shadeMixed = iota + 1
	shadeDark
	shadeFull
)

var (
	// Shared uniform sections are read-only; writableLight copies on mutation.
	// vanilla: DataLayer.fill, DataLayer.getData
	darkSection [lightArrayLen]byte
	fullSection = func() (a [lightArrayLen]byte) {
		for i := range a {
			a[i] = 0xFF
		}
		return a
	}()
)

func (s lightShades) of(c *Chunk) *chunkShades {
	if cs, ok := s[c]; ok {
		return cs
	}
	cs := &chunkShades{}
	for i := range lightSections {
		cs.sky[i] = shadeOf(c.skyLight[i])
		cs.block[i] = shadeOf(c.blockLight[i])
	}
	s[c] = cs
	return cs
}

func shadeOf(a *[lightArrayLen]byte) uint8 {
	switch {
	case lightArrayEmpty(a):
		return shadeDark
	case *a == fullSection:
		return shadeFull
	}
	return shadeMixed
}

// seedAcross queues each cell of column (ox, oz) that would raise the cell one
// step along dir in column (ix, iz). Both chunks are looked up once, and a
// section that cannot seed (outer side with no block light and either no sky
// light or an inner side already at 15) is skipped whole. It reads the chunks'
// own arrays, so it runs before any light is written.
func (ctx *lightCtx) seedAcross(ox, oz, ix, iz, dir int32, q *lightQueue, shades lightShades) {
	oc, ok := ctx.resolve(ox, oz)
	if !ok {
		return
	}
	ic, ok := ctx.resolve(ix, iz)
	if !ok {
		return
	}
	olx, olz, ilx, ilz := ox&15, oz&15, ix&15, iz&15
	outer, inner := shades.of(oc), shades.of(ic)
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		if lightLocalY(y) == 0 {
			si := lightSectionOf(y)
			if outer.block[si] == shadeDark && (outer.sky[si] == shadeDark || inner.sky[si] == shadeFull) {
				y += sectionSize - 1
				continue
			}
		}
		sky := oc.lightGet(&oc.skyLight, olx, y, olz)
		block := oc.lightGet(&oc.blockLight, olx, y, olz)
		if sky <= 1 && block <= 1 {
			continue
		}
		outerState := oc.sharedBlockAt(olx, y, olz)
		state := ic.sharedBlockAt(ilx, y, ilz)
		if spread(sky, int(dir), outerState, state) > ic.lightGet(&ic.skyLight, ilx, y, ilz) {
			q.push(ox, y, oz, sky, skyLayer)
		}
		if spread(block, int(dir), outerState, state) > ic.lightGet(&ic.blockLight, ilx, y, ilz) {
			q.push(ox, y, oz, block, blockLayer)
		}
	}
}

// relightBox wipes and recomputes all light in the full-height box
// [minX,maxX]×[minZ,maxZ], clipped to loaded chunks, with shell cells seeding
// inflow. The caller holds the world write lock.
func (w *World) relightBox(minX, minZ, maxX, maxZ int32) {
	minCx, minCz := floorDiv(minX, sectionSize), floorDiv(minZ, sectionSize)
	maxCx, maxCz := floorDiv(maxX, sectionSize), floorDiv(maxZ, sectionSize)
	type chunkSnapshot struct {
		pos  ChunkPos
		had  bool // the chunk had encoded light
		ring bool // outside the box: rewritten only where the BFS raised it
	}
	var snapshots []chunkSnapshot
	// Snapshot chunks inside the box (these get wiped and recomputed)...
	for cx := minCx; cx <= maxCx; cx++ {
		for cz := minCz; cz <= maxCz; cz++ {
			pos := ChunkPos{cx, cz}
			loaded, ok := w.chunks[pos]
			if !ok {
				continue
			}
			snapshots = append(snapshots, chunkSnapshot{pos, loaded.chunk.hasLightForms(), false})
		}
	}
	if len(snapshots) == 0 {
		return
	}
	// ...and loaded neighbours within lightRadius, which the BFS can spread
	// into without wiping; their encoded light is rebuilt so cross-border
	// spread reaches the wire.
	snapset := make(map[ChunkPos]bool, len(snapshots))
	for _, s := range snapshots {
		snapset[s.pos] = true
	}
	expandMinCx := floorDiv(addClamped(minX, -lightRadius), sectionSize)
	expandMaxCx := floorDiv(addClamped(maxX, lightRadius), sectionSize)
	expandMinCz := floorDiv(addClamped(minZ, -lightRadius), sectionSize)
	expandMaxCz := floorDiv(addClamped(maxZ, lightRadius), sectionSize)
	for cx := expandMinCx; cx <= expandMaxCx; cx++ {
		for cz := expandMinCz; cz <= expandMaxCz; cz++ {
			pos := ChunkPos{cx, cz}
			if snapset[pos] {
				continue
			}
			loaded, ok := w.chunks[pos]
			if !ok {
				continue
			}
			snapshots = append(snapshots, chunkSnapshot{pos, loaded.chunk.hasLightForms(), true})
			snapset[pos] = true
		}
	}

	w.wipeLightBox(minX, minZ, maxX, maxZ)

	q := lightQueues.Get().(*lightQueue)
	defer q.release()
	var touched []*Chunk
	ctx := &lightCtx{w: w, touched: &touched}
	ctx.fillSkyColumns(minX, minZ, maxX, maxZ, q)
	ctx.scanEmitters(minX, minZ, maxX, maxZ, q)
	ctx.seedShell(minX, minZ, maxX, maxZ, q)

	ctx.runLightBFS(q)

	for _, snap := range snapshots {
		loaded := w.chunks[snap.pos]
		// A ring chunk with no raised cell still holds the light its encoded
		// forms came from.
		if snap.ring && snap.had && !slices.Contains(touched, loaded.chunk) {
			loaded.chunk.lightCorrect = true
			loaded.chunk.lightGen++
			continue
		}
		loaded.chunk.invalidateLightForms()
		loaded.chunk.lightCorrect = true
		loaded.chunk.lightGen++
	}
}

// lightAlone lights a chunk not yet in the world from its own blocks,
// neighbours unloaded: the bulk of the work, without w.mu, so prepared chunks
// light in parallel. Its join (newLightJoin) finishes it as it enters the map.
func (w *World) lightAlone(c *Chunk) {
	c.lightDirty = [sectionSize]uint16{}
	c.lightCells, c.lightOverflow = nil, false
	c.lightBands = 0
	c.skyLight = [lightSections]*[lightArrayLen]byte{}
	c.blockLight = [lightSections]*[lightArrayLen]byte{}
	minX, minZ := c.x*16, c.z*16
	q := lightQueues.Get().(*lightQueue)
	defer q.release()
	ctx := &lightCtx{w: w, alone: c}
	ctx.fillSkyColumns(minX, minZ, minX+15, minZ+15, q)
	ctx.scanEmitters(minX, minZ, minX+15, minZ+15, q)
	ctx.runLightBFS(q)
}

// joinBorders runs light across the chunk's four borders, both ways.
func (ctx *lightCtx) joinBorders(pos ChunkPos) {
	minX, minZ := pos.X*16, pos.Z*16
	maxX, maxZ := minX+15, minZ+15
	q := lightQueues.Get().(*lightQueue)
	defer q.release()
	ctx.seedShell(minX, minZ, maxX, maxZ, q)
	shades := lightShades{}
	west, east, north, south := boxSides(minX, minZ, maxX, maxZ)
	for i := range int32(sectionSize) {
		z := minZ + i
		if west {
			ctx.seedAcross(minX, z, minX-1, z, dirWest, q, shades)
		}
		if east {
			ctx.seedAcross(maxX, z, maxX+1, z, dirEast, q, shades)
		}
	}
	for i := range int32(sectionSize) {
		x := minX + i
		if north {
			ctx.seedAcross(x, minZ, x, minZ-1, dirNorth, q, shades)
		}
		if south {
			ctx.seedAcross(x, maxZ, x, maxZ+1, dirSouth, q, shades)
		}
	}
	ctx.runLightBFS(q)
}
