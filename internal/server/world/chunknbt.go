package world

import (
	"fmt"
	"maps"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// dataVersion is the world version written, as internal/server/playerdata.go stamps
// player files: the jar's version.json world_version (5023 for 26.3), not the
// protocol number, which the game uses to decide what a world needs upgraded.
const dataVersion = 5023

// chunkRootKeysModelled are the root tags encodeChunkNBTWith writes from the
// chunk's fields, so a decode keeps them out of carriedRoot instead of holding
// a stale copy the next save would overwrite.
var chunkRootKeysModelled = map[string]bool{
	"DataVersion":    true,
	"xPos":           true,
	"yPos":           true,
	"zPos":           true,
	"Status":         true,
	"LastUpdate":     true,
	"InhabitedTime":  true,
	"plugins":        true,
	"sections":       true,
	"Heightmaps":     true,
	"InfiniBurn":     true,
	"lightCorrect":   true,
	"LightData":      true,
	"block_entities": true,
	"entities":       true,
}

// chunkRootKeysNotCarried are root tags deliberately not carried. block_ticks
// and fluid_ticks are written from the physics queue.
//
// isLightOn is dropped because vanilla reads it as lightCorrect
// (SerializableChunkData.java:121,224) and skips relighting, expecting light in
// each section's BlockLight and SkyLight; this server keeps light in its
// private LightData key, so a carried isLightOn would load in vanilla as lit
// with no light.
var chunkRootKeysNotCarried = map[string]bool{
	"block_ticks": true,
	"fluid_ticks": true,
	"isLightOn":   true,
}

// encodeChunkNBTWith builds a chunk's on-disk root: DataVersion, position,
// status and a section compound each with named block-state and biome palettes.
//
// Live block entities are written from what they hold, unmodelled kinds as the
// compounds read, so a vanilla world keeps its signs and trial spawners; the
// entities list and unmodelled root tags (structures, PostProcessing,
// carving_mask, UpgradeData) are carried for the same reason.
//
// live is the live entities' compounds, read under their own locks without a
// world lock; the rest is read under the caller's world read lock so SetBlock
// or a relight cannot change it mid-encode. This is saveChunk's two-phase
// order.
func encodeChunkNBTWith(c *Chunk, live []nbt.Tag, ticks chunkTicks) (nbt.Compound, error) {
	sections := make([]nbt.Tag, 0, sectionCount)
	bottomY := int32(worldMinY / sectionSize)
	for i := range c.sections {
		sections = append(sections, encodeSectionNBT(&c.sections[i], bottomY+int32(i)))
	}

	// Unmodelled tags go back out as read, under the fresh root written over
	// them.
	root := nbt.Compound{}
	maps.Copy(root, c.savedRoot())
	// A chunk from another version keeps its claim, so the data it carries
	// unread still asks for the game's upgrade; a generated chunk has no claim
	// and takes this one.
	root["DataVersion"] = nbt.Int(c.claimedVersion())
	root["xPos"] = nbt.Int(c.x)
	root["yPos"] = nbt.Int(bottomY)
	root["zPos"] = nbt.Int(c.z)
	root["Status"] = nbt.String("minecraft:full")
	root["LastUpdate"] = nbt.Long(c.lastUpdate)
	root["InhabitedTime"] = nbt.Long(c.inhabitedTime)
	if len(c.pluginData) > 0 {
		root["plugins"] = c.pluginData
	}
	root["sections"] = nbt.List{Elem: nbt.TypeCompound, Items: sections}
	// The client recomputes heightmaps from the blocks, so a written chunk
	// carries an empty compound.
	root["Heightmaps"] = nbt.Compound{}
	// Tick delays are relative to this save's game time.
	// vanilla: SerializableChunkData.copyOf (the packed ticks)
	if len(ticks.block) > 0 {
		root["block_ticks"] = encodeSavedTicks(ticks.block)
	}
	if len(ticks.fluid) > 0 {
		root["fluid_ticks"] = encodeSavedTicks(ticks.fluid)
	}
	root["InfiniBurn"] = nbt.String("minecraft:infiniburn_overworld")
	// Cache the light so a reload need not recompute it: the wire-format bytes
	// are stored verbatim and decoded on load, lightCorrect marking them valid.
	if c.lightCorrect && c.hasLightForms() {
		root["lightCorrect"] = nbt.Byte(1)
		root["LightData"] = nbt.ByteArray(c.lightForm())
	}
	pistonTags := c.encodePistonRides()
	blockEntities := make([]nbt.Tag, 0, len(live)+len(pistonTags)+len(c.carriedBlockEntities.Items))
	// A file older than the furnace burn-time fixer gets furnace progress under
	// the old names, so the fixer does not run over the current ones.
	if c.claimedVersion() < furnaceBurnTimeVersion {
		for _, entry := range live {
			if compound, ok := entry.(nbt.CompoundTag); ok {
				unfixFurnaceBurnTime(compound)
			}
		}
	}
	blockEntities = append(blockEntities, live...)
	blockEntities = append(blockEntities, pistonTags...)
	blockEntities = append(blockEntities, c.carriedBlockEntities.Items...)
	if len(blockEntities) > 0 {
		root["block_entities"] = nbt.List{Elem: nbt.TypeCompound, Items: blockEntities}
	}
	if c.carriedEntities.Items != nil {
		root["entities"] = c.carriedEntities
	}
	return root, nil
}

// claimedVersion is the file's DataVersion claim, or the current one when there
// is none (generated, or a zero claim, which level.dat's rule reads alike).
func (c *Chunk) claimedVersion() int32 {
	if c.dataVersion != 0 {
		return c.dataVersion
	}
	return dataVersion
}

// encodeSectionNBT writes a section: its Y, block_states (named palette, plus
// packed longs if more than one state) and biomes (one name per distinct biome,
// plus packed per-cell indices if mixed).
func encodeSectionNBT(s *section, y int32) nbt.Compound {
	palette, indices := s.blockPalette()

	paletteEntries := make([]nbt.Tag, len(palette))
	paletteType := nbt.TypeString
	for i, state := range palette {
		paletteEntries[i] = blockPaletteEntry(state)
		if paletteEntries[i].Type() != nbt.TypeString {
			paletteType = nbt.TypeCompound
		}
	}
	// Defer mixed-list wrappers to the encoder so they need not be allocated.
	blockStates := nbt.Compound{
		"palette": nbt.List{Elem: paletteType, Items: paletteEntries},
	}
	if len(palette) > 1 {
		bits := max(minIndirectBits, bits.Len(uint(len(palette)-1)))
		blockStates["data"] = packLongs(indices, bits)
	}
	if len(indices) == blocksPerSection {
		indexScratch.Put(&indices)
	}

	biomes := nbt.Compound{
		"palette": nbt.List{
			Elem:  nbt.TypeString,
			Items: []nbt.Tag{nbt.String(biomeName(s.biome))},
		},
	}
	if s.biomes != nil {
		// One packed index per cell, at the width the palette's entries need.
		// A cell array holding one biome (a whole-section /fillbiome) has no
		// width to pack at, so it saves as the single-entry form.
		var palette [biomesPerSection]int32
		var cellIndices [biomesPerSection]uint16
		cellPalette := palette[:s.biomePalette(&palette, &cellIndices)]
		entries := make([]nbt.Tag, len(cellPalette))
		for i, biome := range cellPalette {
			entries[i] = nbt.String(biomeName(biome))
		}
		biomes = nbt.Compound{"palette": nbt.List{Elem: nbt.TypeString, Items: entries}}
		if len(cellPalette) > 1 {
			biomes["data"] = packLongs(cellIndices[:], bits.Len(uint(len(cellPalette)-1)))
		}
	}

	return nbt.Compound{
		"Y":            nbt.Byte(y),
		"block_states": blockStates,
		"biomes":       biomes,
	}
}

// blockPalette returns the section's distinct states in first-seen order and
// each block's index; a uniform (nil blocks) section gets a one-entry palette
// and no indices.
func (s *section) blockPalette() (palette []uint16, indices []uint16) {
	blocks := s.blocks.Load()
	if blocks == nil {
		return []uint16{s.fill}, nil
	}
	return mixedPalette(blocks)
}

// mixedPalette is blockPalette for a section with a block array.
func mixedPalette(blocks *blockWords) (palette []uint16, indices []uint16) {
	var states [blocksPerSection]uint16
	blocks.unpack(&states)
	palette, slots := paletteOf(&states)
	defer releasePalette(palette, slots)
	indices = *indexScratch.Get().(*[]uint16)
	for i := range indices {
		indices[i] = slots[states[i]] - 1
	}
	return palette, indices
}

// indexScratch holds mixedPalette's index slices; encodeSectionNBT returns
// them once packed.
var indexScratch = sync.Pool{New: func() any { s := make([]uint16, blocksPerSection); return &s }}

// paletteOf returns the distinct states in first-seen order and a table of
// each one's index plus one (zero for absent). A table, not a map, because the
// lookup runs for every block of every section sent or saved; return it through
// releasePalette.
func paletteOf(states *[blocksPerSection]uint16) ([]uint16, *[1 << 16]uint16) {
	slots := paletteSlots.Get().(*[1 << 16]uint16)
	palette := make([]uint16, 0, 8)
	add := func(state uint16) {
		if slots[state] == 0 {
			palette = append(palette, state)
			slots[state] = uint16(len(palette))
		}
	}
	for _, state := range states {
		add(state)
	}
	return palette, slots
}

// releasePalette clears the entries paletteOf set and returns the table to the
// pool, where tables are all zero.
func releasePalette(palette []uint16, slots *[1 << 16]uint16) {
	for _, state := range palette {
		slots[state] = 0
	}
	paletteSlots.Put(slots)
}

var paletteSlots = sync.Pool{New: func() any { return new([1 << 16]uint16) }}

// blockPaletteEntry is a state's compact codec tag.
// An unknown ID (newer version or mod) is written as air so a vanilla client
// can still read the chunk.
//
// The tags are cached and shared: a caller must not write into one.
func blockPaletteEntry(stateID uint16) nbt.Tag {
	if int(stateID) >= registry.StateCount {
		stateID = airState
	}
	if e := paletteEntryCache[stateID].Load(); e != nil {
		return *e
	}
	entry := buildBlockPaletteEntry(stateID)
	paletteEntryCache[stateID].Store(&entry)
	return entry
}

var paletteEntryCache [registry.StateCount]atomic.Pointer[nbt.Tag]

func buildBlockPaletteEntry(stateID uint16) nbt.Tag {
	return registry.BlockStateCodecTag(stateID)
}

// packLongs packs indices into a long array at bitsPerEntry bits from the low
// bit up, never crossing a long boundary: putPacked's wire layout, as an NBT
// long array.
func packLongs(indices []uint16, bitsPerEntry int) nbt.LongArray {
	perLong := 64 / bitsPerEntry
	longs := make(nbt.LongArray, 0, (len(indices)+perLong-1)/perLong)
	for i := 0; i < len(indices); i += perLong {
		var packed uint64
		for j := 0; j < perLong && i+j < len(indices); j++ {
			packed |= uint64(indices[i+j]) << (j * bitsPerEntry)
		}
		longs = append(longs, int64(packed))
	}
	return longs
}

// biomeName is a biome ID's registry name, or plains for an out-of-range ID
// (what a newer world's unknown biome reduces to).
func biomeName(id int32) string {
	reg, ok := registry.Get("minecraft:worldgen/biome")
	if !ok {
		return "minecraft:plains"
	}
	if id < 0 || int(id) >= len(reg.Entries) {
		return reg.Entries[plainsBiome]
	}
	return reg.Entries[id]
}

// biomeID is a biome name's ID, or plains if unregistered.
func biomeID(name string) int32 {
	reg, ok := registry.Get("minecraft:worldgen/biome")
	if !ok {
		return plainsBiome
	}
	id := reg.ID(name)
	if id < 0 {
		return plainsBiome
	}
	return id
}

// chunkDataVersionFloor is the oldest chunk DataVersion read: the first after
// the Level unwrapping and the height, biome and tick list fixes this server
// does not carry (1.18 is 2860). The fixes after it are fixOldChunkRoot,
// fixLegacyPalette and fixOldBlockEntity.
// vanilla: DataFixers.addFixers (v2842 ChunkRenamesFix, v2843 OverreachingTickFix)
const chunkDataVersionFloor = 2843

// entityDataVersionFloor is the oldest DataVersion of a stored entity read:
// older ones hold item stacks this server does not componentize.
// vanilla: DataFixers.addFixers (v3692)
const entityDataVersionFloor = 3692

// Data versions of the block renames a chunk palette from below them gets.
const (
	creakingHeartStateVersion = 4294 // vanilla: DataFixers.addFixers (CreakingHeartBlockStateFix)
	testBlockModeVersion      = 4305 // vanilla: DataFixers.addFixers (rename test block mode)
	ironChainVersion          = 4541 // vanilla: DataFixers.addFixers (Rename chain to iron_chain)
)

// fixLegacyPalette renames, in place and keeping each entry's key spelling, the
// blocks and properties a palette of the given data version predates.
// vanilla: LegacyBlockPropertyRenameAndFix, LegacyBlockRenameFix
func fixLegacyPalette(palette nbt.List, version int32) {
	if version >= ironChainVersion {
		return
	}
	for _, item := range palette.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		nameKey, propsKey := "id", "properties"
		name, ok := nbt.Get[nbt.String](entry, nameKey)
		if !ok {
			nameKey, propsKey = "Name", "Properties"
			name, _ = nbt.Get[nbt.String](entry, nameKey)
		}
		props, _ := nbt.Get[nbt.Compound](entry, propsKey)
		if wallBlocks[registry.Qualify(string(name))] {
			fixWallSides(props, version)
		}
		if fix, ok := legacyPaletteFixes[registry.Qualify(string(name))]; ok {
			fix(entry, props, nameKey, version)
		}
	}
}

// legacyPaletteFixes are fixLegacyPalette's fixes by the block name they
// match. A data fixer matches names from before the upgrade, which need not
// be blocks any more, so the key is a string rather than a registry.Block.
var legacyPaletteFixes = map[string]func(entry, props nbt.Compound, nameKey string, version int32){
	"minecraft:creaking_heart": func(_, props nbt.Compound, _ string, version int32) {
		if active, ok := props["active"]; ok && version < creakingHeartStateVersion {
			delete(props, "active")
			state := "uprooted"
			if s, _ := active.(nbt.String); s == "true" {
				state = "awake"
			}
			props["creaking_heart_state"] = nbt.String(state)
		}
	},
	"minecraft:test_block": func(_, props nbt.Compound, _ string, version int32) {
		if mode, ok := props["test_block_mode"]; ok && version < testBlockModeVersion {
			delete(props, "test_block_mode")
			props["mode"] = mode
		}
	},
	"minecraft:redstone_wire": func(_, props nbt.Compound, _ string, version int32) { fixRedstoneSides(props, version) },
	"minecraft:crimson_fungi": func(entry, _ nbt.Compound, nameKey string, version int32) {
		if version < fungusRenameVersion {
			entry[nameKey] = nbt.String("minecraft:crimson_fungus")
		}
	},
	"minecraft:warped_fungi": func(entry, _ nbt.Compound, nameKey string, version int32) {
		if version < fungusRenameVersion {
			entry[nameKey] = nbt.String("minecraft:warped_fungus")
		}
	},
	"minecraft:soul_fire_torch":      renameSoulFire("minecraft:soul_torch"),
	"minecraft:soul_fire_wall_torch": renameSoulFire("minecraft:soul_wall_torch"),
	"minecraft:soul_fire_lantern":    renameSoulFire("minecraft:soul_lantern"),
	"minecraft:grass_path": func(entry, _ nbt.Compound, nameKey string, version int32) {
		if version < grassPathRenameVersion {
			entry[nameKey] = nbt.String("minecraft:dirt_path")
		}
	},
	"minecraft:cauldron": func(entry, props nbt.Compound, nameKey string, version int32) {
		if version >= cauldronRenameVersion {
			return
		}
		// vanilla: CauldronRenameFix
		if level, _ := props["level"].(nbt.String); level == "" || level == "0" {
			delete(entry, "Properties")
			delete(entry, "properties")
		} else {
			entry[nameKey] = nbt.String("minecraft:water_cauldron")
		}
	},
	"minecraft:grass": func(entry, _ nbt.Compound, nameKey string, version int32) {
		if version < grassRenameVersion {
			entry[nameKey] = nbt.String("minecraft:short_grass")
		}
	},
	"minecraft:chain": func(entry, _ nbt.Compound, nameKey string, _ int32) {
		entry[nameKey] = nbt.String("minecraft:iron_chain")
	},
}

// renameSoulFire is the fix that renames a block from before
// soulFireRenameVersion to name.
func renameSoulFire(name string) func(entry, props nbt.Compound, nameKey string, version int32) {
	return func(entry, _ nbt.Compound, nameKey string, version int32) {
		if version < soulFireRenameVersion {
			entry[nameKey] = nbt.String(name)
		}
	}
}

// checkChunkFormat rejects a record older than decodeChunkNBT reads, a
// DataVersion below chunkDataVersionFloor or below levelChunkFloor for a Level
// wrapper, or a Status without namespace from a version that wrote one. A
// chunk in the Level layout is rewritten in place to the current one.
func checkChunkFormat(root nbt.Compound) error {
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	if _, ok := root["Level"]; ok {
		// A Level wrapper with no DataVersion is from before 1.9.
		if version < levelChunkFloor {
			return &unsupportedVersionError{version: int32(version)}
		}
		if err := unwrapLevelChunk(root, int32(version)); err != nil {
			return err
		}
	} else if version != 0 && version < chunkDataVersionFloor {
		return &unsupportedVersionError{version: int32(version)}
	}
	if status, ok := nbt.Get[nbt.String](root, "Status"); ok && !strings.Contains(string(status), ":") && (version == 0 || version >= chunkStatusVersion) {
		return fmt.Errorf("chunk status %q has no namespace", status)
	}
	if version != 0 && version < storedItemVersion {
		if be, ok := nbt.Get[nbt.List](root, "block_entities"); ok && holdsUnfixableItem(be) {
			return &unsupportedVersionError{version: int32(version)}
		}
	}
	return nil
}

// unsupportedVersionError is a record saved before chunkDataVersionFloor,
// which this server has no data fixers for.
type unsupportedVersionError struct{ version int32 }

func (e *unsupportedVersionError) Error() string {
	return fmt.Sprintf("chunk data version %d is below %d", e.version, chunkDataVersionFloor)
}

// chunkIsFull reports whether the root is a readable, fully generated chunk;
// a Status other than "minecraft:full" is (false, nil).
func chunkIsFull(root nbt.Compound) (bool, error) {
	if err := checkChunkFormat(root); err != nil {
		return false, err
	}
	fixOldChunkRoot(root)
	status, _ := nbt.Get[nbt.String](root, "Status")
	return string(status) == "minecraft:full", nil
}

// sectionBlockStates returns a section's block_states with the legacy palette
// renames of dataVersion (0: unknown) applied.
func sectionBlockStates(sec nbt.Compound, dataVersion int32) (nbt.Compound, bool) {
	bs, ok := nbt.Get[nbt.Compound](sec, "block_states")
	if ok && dataVersion != 0 {
		palette, _ := nbt.Get[nbt.List](bs, "palette")
		fixLegacyPalette(palette, dataVersion)
	}
	return bs, ok
}

// decodeChunkNBT reads a chunk's root into a Chunk with its sections, biome
// cells and carried block entities; the world that loads it does the wire
// encoding, since a narrower dimension encodes only its band.
//
// A Status other than "minecraft:full" returns (nil, nil), so the caller
// generates it like a never-saved chunk: vanilla worlds hold half-generated
// chunks ("minecraft:empty", "minecraft:carved", ...) that would load all air
// or half built.
//
// An older format, or an unknown block name or property, is an error: only the
// block renames since chunkDataVersionFloor are fixed, and reading it as air or regenerating would let the next save
// replace the original. Sections outside the overworld's Y range are skipped.
func decodeChunkNBT(root nbt.Compound) (*Chunk, error) {
	if full, err := chunkIsFull(root); !full {
		return nil, err
	}

	x, _ := nbt.Get[nbt.Int](root, "xPos")
	z, _ := nbt.Get[nbt.Int](root, "zPos")
	c := newChunk(int32(x), int32(z))
	c.pluginData, _ = nbt.Get[nbt.Compound](root, "plugins")
	// Keep the file's claim for the save: data carried unread is still in that
	// version's shape.
	if version, ok := nbt.Get[nbt.Int](root, "DataVersion"); ok && version != 0 {
		c.dataVersion = int32(version)
	}
	// LastUpdate as read; a save stamps the world's game time over it
	// (saveChunk).
	if lastUpdate, ok := nbt.Get[nbt.Long](root, "LastUpdate"); ok {
		c.lastUpdate = int64(lastUpdate)
	}
	// The stored age is where the next pass's counting resumes.
	if inhabited, ok := nbt.Get[nbt.Long](root, "InhabitedTime"); ok {
		c.inhabitedTime = int64(inhabited)
	}

	sections, _ := nbt.Get[nbt.List](root, "sections")
	for _, item := range sections.Items {
		sec, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		y, _ := nbt.Get[nbt.Byte](sec, "Y")
		idx := int32(y) - worldMinY/sectionSize
		if idx < 0 || idx >= sectionCount {
			continue
		}
		s := &c.sections[idx]

		if bs, ok := sectionBlockStates(sec, c.dataVersion); ok {
			if err := decodeSectionBlocks(s, bs); err != nil {
				return nil, fmt.Errorf("section %d: %w", int32(y), err)
			}
		}
		if bi, ok := nbt.Get[nbt.Compound](sec, "biomes"); ok {
			decodeSectionBiomes(s, bi)
		}
	}

	// Modelled block entities become live; the rest are kept as read so a
	// vanilla world keeps its trial spawners and beehives. The entities list is
	// carried whole.
	var carriedBlockEntities []nbt.Tag
	if be, ok := nbt.Get[nbt.List](root, "block_entities"); ok {
		for _, entry := range be.Items {
			compound, ok := entry.(nbt.CompoundTag)
			if !ok {
				carriedBlockEntities = append(carriedBlockEntities, entry)
				continue
			}
			rebaseBlockEntityPos(compound, c.x, c.z)
			fixOldBlockEntity(compound, c.dataVersion)
			if c.dataVersion != 0 && c.dataVersion < furnaceBurnTimeVersion {
				fixFurnaceBurnTime(compound)
			}
			if ride, ok := decodePistonRide(compound.Entries()); ok {
				c.setPistonRide(ride)
				continue
			}
			if live, ok := decodeBlockEntity(compound.Entries()); ok {
				c.setBlockEntity(live)
				continue
			}
			carriedBlockEntities = append(carriedBlockEntities, compound.Entries())
		}
	}
	c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound, Items: carriedBlockEntities}
	c.countTickingCarried()
	if ent, ok := nbt.Get[nbt.List](root, "entities"); ok {
		c.carriedEntities = ent
	}

	// Load cached light if this server wrote it; a vanilla chunk has no
	// LightData and is relit in Acquire.
	if lightData, ok := nbt.Get[nbt.ByteArray](root, "LightData"); ok {
		if c.decodeLight(lightData) {
			c.lightFormsInit = true
			c.lightCorrect = true
		}
	}

	for _, t := range decodeSavedTicks(root, "block_ticks") {
		if t.x>>4 == c.x && t.z>>4 == c.z {
			c.loadedTicks = append(c.loadedTicks, loadedTick{savedTick: t})
		}
	}
	for _, t := range decodeSavedTicks(root, "fluid_ticks") {
		if t.x>>4 == c.x && t.z>>4 == c.z {
			c.loadedTicks = append(c.loadedTicks, loadedTick{savedTick: t, fluid: true})
		}
	}

	c.carriedRoot = carryChunkRoot(root)
	if c.dataVersion != 0 && c.dataVersion < oldChunkFixedVersion {
		c.dataVersion = oldChunkFixedVersion
	}

	return c, nil
}

// carryChunkRoot returns every root tag except the modelled and the
// deliberately dropped ones.
func carryChunkRoot(root nbt.Compound) nbt.Compound {
	carried := nbt.Compound{}
	for name, tag := range root {
		if chunkRootKeysModelled[name] || chunkRootKeysNotCarried[name] {
			continue
		}
		carried[name] = tag
	}
	return carried
}

// blockStatesLayout reads a section's block_states palette as state IDs and
// checks its packed data covers every cell. data is empty for a uniform
// section; stateIDs is empty for one with no palette.
func blockStatesLayout(bs nbt.Compound) (stateIDs []uint16, data nbt.LongArray, bitsPerEntry int, err error) {
	palette, _ := nbt.Get[nbt.List](bs, "palette")
	if len(palette.Items) == 0 {
		return nil, nil, 0, nil
	}
	stateIDs = make([]uint16, len(palette.Items))
	for i, entry := range palette.Items {
		id, ok := resolveBlockState(entry)
		if !ok {
			return nil, nil, 0, fmt.Errorf("unknown block state in palette entry %d", i)
		}
		stateIDs[i] = id
	}
	data, _ = nbt.Get[nbt.LongArray](bs, "data")
	if len(data) == 0 {
		return stateIDs, nil, 0, nil
	}
	bitsPerEntry = max(minIndirectBits, bits.Len(uint(len(stateIDs)-1)))
	perLong := 64 / bitsPerEntry
	if need := (blocksPerSection + perLong - 1) / perLong; len(data) < need {
		return nil, nil, 0, fmt.Errorf("block_states data holds %d longs, %d entries at %d bits per entry need %d",
			len(data), blocksPerSection, bitsPerEntry, need)
	}
	return stateIDs, data, bitsPerEntry, nil
}

// blockStatesAt reads the states of the given cells (y<<8|z<<4|x) of a
// section's block_states without building the section; a cell index outside
// the palette reads as air, as in decodeSectionBlocks.
func blockStatesAt(bs nbt.Compound, cells []int, out []uint16) error {
	stateIDs, data, bitsPerEntry, err := blockStatesLayout(bs)
	if err != nil {
		return err
	}
	for i, cell := range cells {
		switch {
		case len(stateIDs) == 0:
			out[i] = airState
		case len(data) == 0:
			out[i] = stateIDs[0]
		default:
			perLong := 64 / bitsPerEntry
			word := uint64(data[cell/perLong]) >> (cell % perLong * bitsPerEntry)
			out[i] = airState
			if idx := int(word & (1<<bitsPerEntry - 1)); idx < len(stateIDs) {
				out[i] = stateIDs[idx]
			}
		}
	}
	return nil
}

// decodeSectionBlocks fills a section from block_states: a single-entry palette
// without data fills it whole, otherwise the longs unpack to palette indices.
// The file has no bits-per-entry, so the palette size decides it, and an array
// too short for 4096 entries at that width is refused rather than read past.
// vanilla: PalettedContainer.unpack
func decodeSectionBlocks(s *section, bs nbt.Compound) error {
	stateIDs, data, bitsPerEntry, err := blockStatesLayout(bs)
	if err != nil || len(stateIDs) == 0 {
		return err
	}
	if len(data) == 0 {
		s.setAll(stateIDs[0])
		return nil
	}
	perLong := 64 / bitsPerEntry

	// Counts must represent all 4096 cells, including an explicit uniform array.
	counts := make([]uint32, len(stateIDs))
	blocks := adoptPackedBlocks(stateIDs, data, bitsPerEntry, counts)
	if blocks == nil {
		clear(counts)
		blocks = newBlockWords(stateIDs[0])
		mask := uint64(1)<<bitsPerEntry - 1
		i := 0
		for _, long := range data {
			word := uint64(long)
			for j := 0; j < perLong && i < blocksPerSection; j++ {
				pIdx := int(word & mask)
				word >>= bitsPerEntry
				state := uint16(airState)
				if pIdx < len(stateIDs) {
					state = stateIDs[pIdx]
					counts[pIdx]++
				}
				blocks = blocks.put(i, state, false)
				i++
			}
			if i == blocksPerSection {
				break
			}
		}
	}
	s.fill = stateIDs[0]
	s.spawn.Store(nil)
	s.tickMask.Store(nil)
	s.blocks.Store(blocks)
	s.ticking, s.emitting, s.rods, s.eggs, s.portals, s.pois = 0, 0, 0, 0, 0, 0
	s.sculk = nil
	s.touches.Store(0)
	s.shapes.Store(0)
	s.colliding, s.comparators = 0, 0
	var sculkCounts [sculkCellKinds]int
	var touches, shapes int32
	// Invalid indices read as air, which contributes to none of these tallies.
	for pIdx, state := range stateIDs {
		if n := int(counts[pIdx]); n > 0 {
			if kind := s.countState(state, n, &touches, &shapes); kind != noSculkCell {
				sculkCounts[kind] += n
			}
		}
	}
	s.touches.Store(touches)
	s.shapes.Store(shapes)
	if sculkCounts != [sculkCellKinds]int{} {
		s.indexDecodedSculkCells(blocks, sculkCounts)
	}
	return nil
}

// adoptPackedBlocks builds storage straight from the saved words when the file's
// palette already is the in-memory layout: width 4-8, distinct states and every
// index inside the palette. It counts each index into counts and returns nil
// (counts then meaningless) for anything else, which the cell-by-cell decoder
// handles. data holds at least a section's words.
// vanilla: PalettedContainer.unpack
func adoptPackedBlocks(stateIDs []uint16, data nbt.LongArray, bitsPerEntry int, counts []uint32) *blockWords {
	if bitsPerEntry > maxIndirectBits {
		return nil
	}
	b := makeBlockWords(bitsPerEntry)
	for _, state := range stateIDs {
		if b.paletteSlot(state) != b.used {
			return nil
		}
		b.palette[b.used] = uint32(state)
		b.indexSlot(state, b.used)
		b.used++
	}
	words := data[:len(b.words)]
	mask, n := b.mask, len(stateIDs)
	cell := 0
	for w, long := range words {
		word := uint64(long)
		take := min(b.perWord, blocksPerSection-cell)
		for range take {
			pIdx := int(word & mask)
			if pIdx >= n {
				return nil
			}
			counts[pIdx]++
			word >>= bitsPerEntry
		}
		if take < b.perWord {
			// Padding past the last cell is not part of the section.
			long &= int64(1)<<(take*bitsPerEntry) - 1
		}
		b.words[w] = uint64(long)
		cell += take
	}
	return b
}

// packedIndices reads count indices of bitsPerEntry bits from a packed long
// array (packLongs' layout); a short array reads zero, the palette's first
// entry, for cells it does not reach.
func packedIndices(data nbt.LongArray, bitsPerEntry, count int) []uint16 {
	perLong := 64 / bitsPerEntry
	mask := uint64(1)<<bitsPerEntry - 1
	indices := make([]uint16, count)
	for i := range count {
		longIdx, bitIdx := i/perLong, (i%perLong)*bitsPerEntry
		if longIdx >= len(data) {
			break
		}
		indices[i] = uint16(uint64(data[longIdx]) >> bitIdx & mask)
	}
	return indices
}

// decodeSectionBiomes fills the 4x4x4 biome cells from biomes: a single entry
// covers the section; otherwise packed data whose width comes from the palette
// size alone, not the long count. An index past the palette, or data short of
// the grid, reads the first biome rather than failing the load.
// vanilla: PalettedContainer.unpack
func decodeSectionBiomes(s *section, bi nbt.Compound) {
	palette, _ := nbt.Get[nbt.List](bi, "palette")
	ids := make([]int32, 0, len(palette.Items))
	for _, entry := range palette.Items {
		// A non-name entry becomes plains instead of being dropped, so later
		// entries keep their indices.
		name, _ := entry.(nbt.String)
		ids = append(ids, biomeID(string(name)))
	}
	if len(ids) == 0 {
		return
	}

	s.biome = ids[0]
	data, hasData := nbt.Get[nbt.LongArray](bi, "data")
	if len(ids) == 1 || !hasData || len(data) == 0 {
		s.biomes = nil
		return
	}

	cells := make([]int32, biomesPerSection)
	for i := range cells {
		cells[i] = ids[0]
	}
	for i, pIdx := range packedIndices(data, bits.Len(uint(len(ids)-1)), biomesPerSection) {
		if int(pIdx) < len(ids) {
			cells[i] = ids[pIdx]
		}
	}
	if uniform(cells, ids[0]) {
		s.biomes = nil
		return
	}
	s.biomes = cells
}

// uniform reports whether every cell holds the given biome.
func uniform(cells []int32, biome int32) bool {
	for _, cell := range cells {
		if cell != biome {
			return false
		}
	}
	return true
}

// resolveBlockState turns a block state palette entry into a state ID; ok
// is false for an unregistered name, property or value.
func resolveBlockState(entry nbt.Tag) (id uint16, ok bool) {
	switch entry.(type) {
	case nbt.CompoundTag, nbt.String:
	default:
		return airState, false
	}
	name, overrides := registry.BlockStateOf(entry)
	id, ok = registry.FindState(name, overrides)
	if !ok {
		return id, false
	}
	// FindState falls back to the default state for unmatched properties, and
	// reading that would drop the stored values on the next save.
	have := registry.StateProps(id)
	for k, v := range overrides {
		if have[k] != v {
			return airState, false
		}
	}
	return id, true
}

// String form for error messages.
func (c *Chunk) String() string {
	return fmt.Sprintf("chunk(%d,%d)", c.x, c.z)
}

// rebaseBlockEntityPos moves a block entity's x/z into chunk (cx, cz), keeping
// the in-chunk offset.
// vanilla: BlockEntity.getPosFromTag
func rebaseBlockEntityPos(entry nbt.CompoundTag, cx, cz int32) {
	x, _ := nbt.Get[nbt.Int](entry.Entries(), "x")
	z, _ := nbt.Get[nbt.Int](entry.Entries(), "z")
	if int32(x)>>4 == cx && int32(z)>>4 == cz {
		return
	}
	entry.Set("x", nbt.Int(cx<<4|int32(x)&15))
	entry.Set("z", nbt.Int(cz<<4|int32(z)&15))
}
