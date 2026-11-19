package world

import (
	"bytes"
	"os"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The three dimensions: wire names (what Login (play), Respawn and a player
// file's Dimension carry), the dimension types the client resolves from its own
// data pack, and each type's shape, which the wire chunk and light formats vary
// by, since the client reads sections bottom-up from the type's height alone.
//
// In memory every dimension spans the overworld's y=-64..319 so one chunk type
// serves all three, and generators keep to the client's band (nether terrain in
// 0..127 of a 256-tall dimension, the end island in 0..255). The wire and disk
// encoders here cut a chunk to that band: a nether or end chunk leaves as 16
// sections.
const (
	// The overworld, whose name also marks the world holding the clock,
	// difficulty, weather and spawn.
	DimensionName = "minecraft:overworld"
	DimensionType = "minecraft:overworld"

	// The nether.
	NetherName = "minecraft:the_nether"
	NetherType = "minecraft:the_nether"

	// The end.
	EndName = "minecraft:the_end"
	EndType = "minecraft:the_end"
)

// dimensionShape is a dimension's wire and disk shape: its first Y and height
// (from the client's dimension type) and whether it has sky light; the nether
// does not (has_skylight false), so its sky light goes out all zero.
type dimensionShape struct {
	// ceiling is the type's has_ceiling: the nether's.
	ceiling bool
	minY    int32
	height  int32
	// logicalHeight is the span chorus fruit and portals treat as the dimension
	// (logical_height).
	logicalHeight int32
	skyLight      bool
	// fixedTime is the type's has_fixed_time: the nether's and the end's.
	fixedTime bool
	// typ is the dimension type's name, which keys its environment attributes
	// and timelines.
	typ string
	// defaultClock is absent for a type without default_clock.
	defaultClock string
}

// dimensionShapes holds each dimension's shape by wire name, from vanilla's
// data/minecraft/dimension_type/*.json. The nether is 256 tall with a logical
// height of 128; the chunk format follows height, and a client sent fewer
// sections than that drops the connection.
var dimensionShapes = map[string]dimensionShape{
	DimensionName: {minY: worldMinY, height: worldHeight, logicalHeight: worldHeight, skyLight: true, typ: DimensionType, defaultClock: OverworldClock},
	NetherName:    {minY: 0, height: 256, logicalHeight: 128, skyLight: false, ceiling: true, fixedTime: true, typ: NetherType},
	EndName:       {minY: 0, height: 256, logicalHeight: 256, skyLight: true, fixedTime: true, typ: EndType, defaultClock: TheEndClock},
}

// NewDimensionWorld builds the nether or end attached to the world under dir:
// region files in the dimension's folder (layout.go), chunks from gen, and its
// wire shape. It has no level.dat (vanilla's layout keeps exactly one, at the
// overworld's root), so clock, weather, difficulty and spawn are the
// overworld's.
func NewDimensionWorld(dir string, nether bool, gen Generator) *World {
	name := EndName
	if nether {
		name = NetherName
	}
	// This world owns no level.dat, so newWorld cannot fail here.
	w, _ := newWorld(dir, gen, name, dimensionShapes[name], false)
	return w
}

// firstSection is the in-memory section index of the dimension's first wire
// section: whole sections between worldMinY and the dimension's minY.
func (w *World) firstSection() int {
	return int((w.dim.minY - worldMinY) / sectionSize)
}

// Name is the dimension's wire name, as a player file's Dimension holds it.
func (w *World) Name() string {
	return w.dimName
}

// DimensionTypeName is the dimension type's registry name, which keys its
// environment attributes and can differ from the world's dimension name.
func (w *World) DimensionTypeName() string { return w.dim.typ }

// DefaultClock is the dimension type's optional default_clock.
// vanilla: DimensionType.defaultClock
func (w *World) DefaultClock() (string, bool) {
	return w.dim.defaultClock, w.dim.defaultClock != ""
}

// HasCeiling is DimensionType.hasCeiling.
func (w *World) HasCeiling() bool { return w.dim.ceiling }

// CanHaveWeather is Level.canHaveWeather: sky light, no ceiling, and not the
// end. Such a world fades its own rain and thunder levels toward the
// overworld's weather flags.
func (w *World) CanHaveWeather() bool {
	return w.dim.skyLight && !w.dim.ceiling && w.dimName != EndName
}

// MinY is the dimension's first block Y per the client's dimension type (-64
// overworld, 0 otherwise), where iteration over its band begins and the void is
// measured from.
func (w *World) MinY() int32 {
	return w.dim.minY
}

// MaxY returns one past the dimension's top block, Level.getMaxY.
func (w *World) MaxY() int32 {
	return w.dim.minY + w.dim.height
}

// Height is the dimension's height per the client's type: the band of the
// 384-block in-memory chunk the client parses.
func (w *World) Height() int32 {
	return w.dim.height
}

// LogicalHeight is the dimension type's logical_height, how far above MinY
// counts as inhabitable (128 in the nether).
func (w *World) LogicalHeight() int32 {
	return w.dim.logicalHeight
}

// wireSections is how many sections the dimension's chunks carry on the wire.
func (w *World) wireSections() int {
	return int(w.dim.height / sectionSize)
}

// encodeChunkSections writes the chunk's sections over the wire band, bottom to
// top, as Chunk Data carries them; for the overworld the band is all of memory.
// The form is cached on the chunk, so it is built in scratch and copied out at
// its size rather than kept with the room a growing buffer leaves.
func (c *Chunk) encodeChunkSections(from, count int) []byte {
	b := scratchBuffer()
	defer scratchBuffers.Put(b)
	for i := from; i < from+count; i++ {
		c.sections[i].encode(b)
	}
	return bytes.Clone(b.Bytes())
}

// encodeChunkWired is the dimension's encoded form: the wire band its height
// calls for (the full-height encoding, with an offset, for the overworld).
func (w *World) encodeChunkWired(c *Chunk) []byte {
	return c.encodeChunkSections(w.firstSection(), w.wireSections())
}

// encodeChunk caches the chunk's wire forms for this dimension, block sections
// and heightmaps over its band, rebuilt together.
func (w *World) encodeChunk(c *Chunk) {
	c.encoded = w.encodeChunkWired(c)
	c.heightmaps = w.encodeHeightmaps(c)
}

// encodeLightSpan writes wire light data for the dimension's light sections
// only (its sections plus one below and one above, as encodeLight does for all
// of memory), offset by the same firstSection as the blocks, so light section 0
// is the in-memory section below the dimension's floor.
//
// Without sky light, the sky mask stays empty and every sky section goes in the
// empty sky mask, as in the nether, where no light comes from the sky whatever
// is above the bedrock.
func (c *Chunk) encodeLightSpan(offset, sections int, skyLight bool, f lightFilter) []byte {
	var m lightMasks
	sky, block := c.skyLight[offset:offset+sections], c.blockLight[offset:offset+sections]
	for i := range sections {
		if skyLight && sky[i] != nil {
			m.sky |= 1 << i
		} else {
			m.emptySky |= 1 << i
		}
		if block[i] != nil {
			m.block |= 1 << i
		} else {
			m.emptyBlock |= 1 << i
		}
	}
	return m.filter(f).encode(sky, block)
}

// lightWired is a chunk's Update Light payload here: the overworld's cached
// full-height encoding (keeping uniformly lit sections so a changed one is
// corrected), cut to the client's band elsewhere.
func (w *World) lightWired(c *Chunk) []byte {
	if w.dim.minY == worldMinY && w.dim.height == worldHeight {
		return c.lightForm()
	}
	return c.encodeLightSpan(w.firstSection(), w.wireSections()+2, w.dim.skyLight, allLight)
}

// chunkLightWired is a whole-chunk Chunk Data light payload: the overworld
// omits uniformly lit sky sections (safe on an initial send, as the client
// starts sections full); other dimensions send their span form.
func (w *World) chunkLightWired(c *Chunk) []byte {
	if w.dim.minY == worldMinY && w.dim.height == worldHeight {
		return c.chunkLightForm()
	}
	return w.lightWired(c)
}

// UpdateLightPacket builds an Update Light payload for the chunk's changed
// light sections (bit i of sky or block is light section i), in this
// dimension's band. It is nil if the chunk is not loaded or nothing in the
// band changed.
// vanilla: ClientboundLightUpdatePacketData(ChunkPos, LevelLightEngine, BitSet, BitSet)
func (w *World) UpdateLightPacket(pos ChunkPos, sky, block uint32) []byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return nil
	}
	c := loaded.chunk
	// Every light section is in a sky or an empty-sky mask, so a filter with
	// no bit left leaves all four masks empty.
	f := lightFilter{uint64(sky), uint64(block)}
	var data []byte
	if w.dim.minY == worldMinY && w.dim.height == worldHeight {
		if f.sky|f.block == 0 {
			return nil
		}
		data = c.encodeLightForm(false, f)
	} else {
		offset, sections := w.firstSection(), w.wireSections()+2
		span := uint64(1)<<sections - 1
		f = lightFilter{f.sky >> offset & span, f.block >> offset & span}
		if f.sky|f.block == 0 {
			return nil
		}
		data = c.encodeLightSpan(offset, sections, w.dim.skyLight, f)
	}
	var b protocol.Buffer
	b.PutVarInt(pos.X)
	b.PutVarInt(pos.Z)
	b.PutRaw(data)
	return b.Bytes()
}

// clipChunkNBT trims a chunk's disk compound to the dimension's band as vanilla
// stores it: only sections inside, numbered from its own floor. For the
// overworld it returns the compound unchanged.
func (w *World) clipChunkNBT(root nbt.Compound) nbt.Compound {
	if w.dim.minY == worldMinY && w.dim.height == worldHeight {
		return root
	}
	if sections, ok := nbt.Get[nbt.List](root, "sections"); ok {
		firstY := w.dim.minY / sectionSize
		lastY := firstY + int32(w.wireSections())
		kept := make([]nbt.Tag, 0, len(sections.Items))
		for _, item := range sections.Items {
			sec, ok := nbt.CompoundView(item)
			if !ok {
				kept = append(kept, item)
				continue
			}
			y, ok := nbt.Get[nbt.Byte](sec, "Y")
			if !ok || int32(y) < firstY || int32(y) >= lastY {
				continue // outside the dimension's band, as a vanilla world never stores it
			}
			kept = append(kept, item)
		}
		// The list is a value, so put the trimmed form back into the root.
		sections.Items = kept
		root["sections"] = sections
	}
	root["yPos"] = nbt.Int(w.dim.minY / sectionSize)
	return root
}

// VoidY is where the void starts hurting: 64 below the dimension's own floor,
// not the shared in-memory one.
func (w *World) VoidY() int32 {
	return w.dim.minY - 64
}

// FastLava reports the dimension type's minecraft:gameplay/fast_lava (the
// nether only), which the fluid current reads: nether lava pushes a floating
// body three times as hard.
func (w *World) FastLava() bool {
	return w.dimName == NetherName
}

// WaterEvaporates reports the WATER_EVAPORATES attribute (the nether only),
// which a bucket's pour checks.
// vanilla: BucketItem.emptyContents, EnvironmentAttributes.WATER_EVAPORATES
func (w *World) WaterEvaporates() bool {
	return w.dimName == NetherName
}

// RespawnAnchorWorks reports the RESPAWN_ANCHOR_WORKS attribute (the nether
// only); elsewhere a charged anchor's use explodes and an uncharged one does
// nothing.
// vanilla: RespawnAnchorBlock.canSetSpawn,
// EnvironmentAttributes.RESPAWN_ANCHOR_WORKS
func (w *World) RespawnAnchorWorks() bool {
	return w.dimName == NetherName
}

// HasChunk reports whether the chunk is in memory or saved, without generating
// it, so the portal search skips never-saved wilderness (no portal there, and
// loading it would generate it).
func (w *World) HasChunk(pos ChunkPos) bool {
	w.mu.RLock()
	_, loaded := w.chunks[pos]
	w.mu.RUnlock()
	if loaded {
		return true
	}
	rx, rz := regionCoords(pos)
	if _, err := os.Stat(regionFilePath(w.storage, rx, rz)); err != nil {
		return false // no region file, no chunk to load
	}
	rf, err := w.region(rx, rz)
	if err != nil {
		return false
	}
	defer w.releaseRegion(rf)
	data, err := rf.readChunk(pos)
	return err == nil && data != nil
}
