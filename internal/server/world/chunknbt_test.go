package world

import (
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// loadedChunkTicks is the ticks the chunk was read with, as a save would
// list them had the engine taken them unchanged.
func loadedChunkTicks(c *Chunk) chunkTicks {
	var out chunkTicks
	for _, t := range c.loadedTicks {
		if t.fluid {
			out.fluid = append(out.fluid, t.savedTick)
		} else {
			out.block = append(out.block, t.savedTick)
		}
	}
	return out
}

// encodeChunkNBT builds the on-disk NBT compound for a chunk the way the save
// reads it: the live block entities under their own locks, the rest of the
// chunk with no world lock held. It is only the tests' way in.
func encodeChunkNBT(c *Chunk) (nbt.Compound, error) {
	return encodeChunkNBTWith(c, encodeBlockEntities(c.blockEntities()), loadedChunkTicks(c))
}

// TestChunkNBTRoundTrip checks that a chunk encodes to NBT and decodes back with
// the same blocks and biome in every section, which is what a save and a load
// amounts to.
func TestChunkNBTRoundTrip(t *testing.T) {
	t.Parallel()
	original := flatChunk(3, -7)
	original.SetBlock(1, GroundLevel-1, 2, airState) // dig a hole
	original.SetBlock(5, worldMinY, 5, stoneState)   // place a block at the bottom

	root, err := encodeChunkNBT(original)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}

	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("nbt.Decode: %v", err)
	}
	loaded, err := decodeChunkNBT(tag.(nbt.Compound))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	if loaded.X() != 3 || loaded.Z() != -7 {
		t.Errorf("chunk position = (%d, %d), want (3, -7)", loaded.X(), loaded.Z())
	}

	// Every block that was in the original should be in the loaded chunk.
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				want := original.BlockAt(x, y, z)
				if got := loaded.BlockAt(x, y, z); got != want {
					t.Errorf("block at (%d,%d,%d) = %d, want %d", x, y, z, got, want)
				}
			}
		}
	}

	// The biome should survive too.
	if loaded.biome != original.biome {
		t.Errorf("biome = %d, want %d", loaded.biome, original.biome)
	}
}

// TestChunkNBTRoundTripsInhabitedTime checks the age a chunk carries in its
// file: a vanilla chunk loads with the InhabitedTime it saved with, and a
// save writes that age back out rather than dropping it.
func TestChunkNBTRoundTripsInhabitedTime(t *testing.T) {
	t.Parallel()
	root := vanillaChunkWith()
	root["InhabitedTime"] = nbt.Long(3600000)

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	if loaded.inhabitedTime != 3600000 {
		t.Errorf("inhabitedTime = %d, want the 3600000 the chunk loaded with", loaded.inhabitedTime)
	}
	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	if got, ok := nbt.Get[nbt.Long](saved, "InhabitedTime"); !ok || got != 3600000 {
		t.Errorf("saved InhabitedTime = %v, %v; want the 3600000 the chunk loaded with", got, ok)
	}
	// A chunk with no age of its own writes a zero one; the tag rides in
	// every chunk's root. vanilla: SerializableChunkData.write
	fresh, err := encodeChunkNBT(flatChunk(0, 0))
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	if got, ok := nbt.Get[nbt.Long](fresh, "InhabitedTime"); !ok || got != 0 {
		t.Errorf("a fresh chunk's InhabitedTime = %v, %v; want 0", got, ok)
	}
}

// TestChunkNBTRefusesShortBlockStateArray checks that a section whose
// block_states data array is too short to hold the section's 4096 entries is
// refused with an error rather than read past its end. A corrupt or truncated
// file must not panic the server.
func TestChunkNBTRefusesShortBlockStateArray(t *testing.T) {
	t.Parallel()
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(0),
		"yPos":        nbt.Int(worldMinY / sectionSize),
		"zPos":        nbt.Int(0),
		"Status":      nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Y": nbt.Byte(worldMinY / sectionSize),
				"block_states": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
						nbt.Compound{"Name": nbt.String("minecraft:air")},
						nbt.Compound{"Name": nbt.String("minecraft:stone")},
					}},
					"data": nbt.LongArray{1},
				},
				"biomes": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
						nbt.String("minecraft:plains"),
					}},
				},
			},
		}},
	}

	chunk, err := decodeChunkNBT(root)
	if err == nil {
		t.Fatalf("decodeChunkNBT accepted a one-long block_states array, returned %v", chunk)
	}
}

// mixedBiomeSections builds a chunk root whose bottom section's biome grid
// is plains apart from its topmost cell (x=3, z=3, y=3), which is desert,
// and whose section above is wholly swamp: the [plains, desert] / [swamp]
// shapes of the flat file packing, one bit per cell across one long.
func mixedBiomeSections() nbt.Compound {
	bottomY := int32(worldMinY / sectionSize)
	// One bit per entry over 64 cells fills a single long, cell 63 on its
	// high bit.
	var desertCell uint64 = 1
	desertCell <<= 63
	bottom := nbt.Compound{
		"Y": nbt.Byte(bottomY),
		"biomes": nbt.Compound{
			"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String("minecraft:plains"),
				nbt.String("minecraft:desert"),
			}},
			"data": nbt.LongArray{int64(desertCell)},
		},
	}
	above := nbt.Compound{
		"Y": nbt.Byte(bottomY + 1),
		"biomes": nbt.Compound{
			"palette": nbt.List{Elem: nbt.TypeString,
				Items: []nbt.Tag{nbt.String("minecraft:swamp")},
			},
		},
	}
	return nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(0),
		"yPos":        nbt.Int(bottomY),
		"zPos":        nbt.Int(0),
		"Status":      nbt.String("minecraft:full"),
		"sections":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{bottom, above}},
	}
}

// TestChunkNBTKeepsASectionsBiomePalette checks the biome side of a chunk's
// NBT: a section mixing biomes round-trips with its palette and packed cell
// data — the desert cells survive a save — and a loaded chunk's own default
// biome stays the plains it starts with rather than following the last
// section read.
// vanilla: SerializableChunkData stores a section's biomes whole
func TestChunkNBTKeepsASectionsBiomePalette(t *testing.T) {
	t.Parallel()
	plains := registry.MustID("minecraft:worldgen/biome", "minecraft:plains")

	loaded, err := decodeChunkNBT(mixedBiomeSections())
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	// The swamp section above must not become the chunk's own answer.
	if loaded.biome != plains {
		t.Errorf("the chunk's default biome = %d, want the plains it starts with (%d)",
			loaded.biome, plains)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	bottomY := int32(worldMinY / sectionSize)

	// The bottom section still lists both biomes, and the packed data puts
	// the desert back in its own cell.
	bottom, _ := nbt.Get[nbt.Compound](savedSectionByY(t, saved, bottomY), "biomes")
	bottomPalette, _ := nbt.Get[nbt.List](bottom, "palette")
	want := []string{"minecraft:plains", "minecraft:desert"}
	if len(bottomPalette.Items) != len(want) {
		t.Fatalf("the saved bottom palette has %d biomes, want the two it was loaded with", len(bottomPalette.Items))
	}
	for i, name := range want {
		if got, ok := bottomPalette.Items[i].(nbt.String); !ok || string(got) != name {
			t.Errorf("the saved bottom palette entry %d = %#v, want %s", i, bottomPalette.Items[i], name)
		}
	}
	data, hasData := nbt.Get[nbt.LongArray](bottom, "data")
	if !hasData || len(data) != 1 {
		t.Fatalf("the saved bottom biome data = %v, want the one long a 1-bit palette packs 64 cells into", data)
	}
	if uint64(data[0])&(uint64(1)<<63) == 0 {
		t.Errorf("the saved biome data = %#x, want the desert cell on its high bit", uint64(data[0]))
	}

	// The uniform section above keeps the single-entry palette a save has
	// always written for it.
	above, _ := nbt.Get[nbt.Compound](savedSectionByY(t, saved, bottomY+1), "biomes")
	abovePalette, _ := nbt.Get[nbt.List](above, "palette")
	if len(abovePalette.Items) != 1 || string(abovePalette.Items[0].(nbt.String)) != "minecraft:swamp" {
		t.Errorf("the saved swamp palette = %#v, want the single swamp", abovePalette.Items)
	}
	if _, hasData := nbt.Get[nbt.LongArray](above, "data"); hasData {
		t.Error("the saved swamp section carries biome data, want the single-entry palette alone")
	}

	// A second load of the same bytes keeps the shapes the first did.
	reloaded, err := decodeChunkNBT(saved)
	if err != nil {
		t.Fatalf("decode the saved chunk: %v", err)
	}
	resaved, err := encodeChunkNBT(reloaded)
	if err != nil {
		t.Fatalf("re-encode the saved chunk: %v", err)
	}
	resavedBottom, _ := nbt.Get[nbt.Compound](savedSectionByY(t, resaved, bottomY), "biomes")
	resavedPalette, _ := nbt.Get[nbt.List](resavedBottom, "palette")
	if len(resavedPalette.Items) != 2 {
		t.Errorf("the twice-saved palette has %d biomes, want the two still", len(resavedPalette.Items))
	}
	if reloaded.biome != plains {
		t.Errorf("the reloaded chunk's default biome = %d, want the plains still (%d)", reloaded.biome, plains)
	}
}

// savedSectionByY reads the section a saved chunk's NBT carries at the given
// Y, failing the test when no section sits there.
func savedSectionByY(t *testing.T, root nbt.Compound, y int32) nbt.Compound {
	t.Helper()
	sections, ok := nbt.Get[nbt.List](root, "sections")
	if !ok {
		t.Fatalf("saved chunk carries no sections list")
	}
	for _, item := range sections.Items {
		sec, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		if got, _ := nbt.Get[nbt.Byte](sec, "Y"); got == nbt.Byte(y) {
			return sec
		}
	}
	t.Fatalf("the saved chunk has no section at Y=%d: %#v", y, sections)
	return nil
}

// TestChunkNBTReadsAWideBiomePalette decodes a section that holds five
// biomes: the packed data runs three bits per cell — the width the palette
// itself asks for, which the long count would read as four — so the cells
// come back only when the width is taken from the palette.
func TestChunkNBTReadsAWideBiomePalette(t *testing.T) {
	t.Parallel()
	names := []string{
		"minecraft:plains", "minecraft:desert", "minecraft:swamp",
		"minecraft:forest", "minecraft:ocean",
	}
	want := make([]int32, len(names))
	for i, name := range names {
		want[i] = biomeID(name)
	}

	// Cell i holds palette entry i%5, packed by hand at 3 bits each: 21
	// entries to a long, four longs for the grid of 64 cells.
	var data nbt.LongArray
	var packed uint64
	slots := 0
	for i := range 64 {
		packed |= uint64(i%len(names)) << (slots * 3)
		slots++
		if slots == 21 {
			data = append(data, int64(packed))
			packed, slots = 0, 0
		}
	}
	if slots > 0 {
		data = append(data, int64(packed))
	}

	palette := make([]nbt.Tag, len(names))
	for i, name := range names {
		palette[i] = nbt.String(name)
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(0),
		"yPos":        nbt.Int(worldMinY / sectionSize),
		"zPos":        nbt.Int(0),
		"Status":      nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Y": nbt.Byte(worldMinY / sectionSize),
				"biomes": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeString, Items: palette},
					"data":    data,
				},
			},
		}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	// The cells go back out on the wire unchanged, at the 3 bits a palette
	// of five entries needs.
	r := protocol.NewReader(encodeWhole(loaded))
	r.ReadI16() // block count: the section holds none
	r.ReadI16() // fluid count
	readContainer(t, r, blocksPerSection)
	bits, cells := readContainer(t, r, 64)
	if bits != 3 {
		t.Fatalf("the biome container's bits per entry = %d, want 3 for a palette of five", bits)
	}
	for i, cell := range cells {
		if int32(cell) != want[i%len(want)] {
			t.Fatalf("biome cell %d = %d, want %d", i, cell, want[i%len(want)])
		}
	}
}

// TestChunkNBTRoundTripsAWideBiomeGrid saves a section holding nine distinct
// biomes and reads the cells back one by one: nine entries need four bits an
// index, which the long count alone does not show — four longs of sixteen
// four-bit entries could also read as fewer, wider entries — so the grid only
// survives when the width comes from the palette.
func TestChunkNBTRoundTripsAWideBiomeGrid(t *testing.T) {
	t.Parallel()
	names := []string{
		"minecraft:badlands", "minecraft:birch_forest", "minecraft:beach",
		"minecraft:forest", "minecraft:ocean", "minecraft:plains",
		"minecraft:desert", "minecraft:snowy_plains", "minecraft:swamp",
	}

	original := newChunk(4, -9)
	original.sections[0].biomes = make([]int32, biomesPerSection)
	for i := range original.sections[0].biomes {
		original.sections[0].biomes[i] = biomeID(names[i%len(names)])
	}

	root, err := encodeChunkNBT(original)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("nbt.Decode: %v", err)
	}
	loaded, err := decodeChunkNBT(tag.(nbt.Compound))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	bottomY := int32(worldMinY / sectionSize)
	savedBiomes, _ := nbt.Get[nbt.Compound](savedSectionByY(t, root, bottomY), "biomes")
	packed, ok := nbt.Get[nbt.LongArray](savedBiomes, "data")
	if !ok || len(packed) != 4 {
		t.Fatalf("the saved biome data = %v, want the four longs a 4-bit palette packs 64 cells into", packed)
	}

	// Cell i is the block coordinate its x, y and z bits pick, local to the
	// section, and holds the i%9th biome.
	for cell := range biomesPerSection {
		x := int32(cell&3) * 4
		z := int32(cell>>2&3) * 4
		y := int32(cell>>4&3) * 4
		want := biomeID(names[cell%len(names)])
		if got := loaded.BiomeAt(x, worldMinY+y, z); got != want {
			t.Fatalf("cell %d (%d,%d,%d) = %d, want %d", cell, x, y, z, got, want)
		}
	}
}

// TestChunkNBTKeepsBiomeIndicesAfterANonNameEntry checks a corrupt palette: a
// slot that is not a biome name fills in as plains in place rather than being
// dropped, so the entries after it keep their indices and a cell naming the
// last biome still reads it.
func TestChunkNBTKeepsBiomeIndicesAfterANonNameEntry(t *testing.T) {
	t.Parallel()
	plains := registry.MustID("minecraft:worldgen/biome", "minecraft:plains")
	desert := registry.MustID("minecraft:worldgen/biome", "minecraft:desert")

	// Cell 0 names palette entry 2, desert, at two bits an index.
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"xPos":        nbt.Int(0),
		"yPos":        nbt.Int(worldMinY / sectionSize),
		"zPos":        nbt.Int(0),
		"Status":      nbt.String("minecraft:full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Y": nbt.Byte(worldMinY / sectionSize),
				"biomes": nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
						nbt.String("minecraft:plains"),
						nbt.Int(0),
						nbt.String("minecraft:desert"),
					}},
					"data": nbt.LongArray{2},
				},
			},
		}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	if got := loaded.BiomeAt(0, worldMinY, 0); got != desert {
		t.Errorf("biome at the cell naming the last palette entry = %d, want desert (%d)", got, desert)
	}
	if got := loaded.BiomeAt(4, worldMinY, 0); got != plains {
		t.Errorf("biome at a cell naming the first entry = %d, want plains (%d)", got, plains)
	}
}

// TestChunkNBTSerializesBlocksWithProperties checks that a block with
// properties — an oak log on its side — round-trips through NBT with the
// properties preserved, rather than collapsing to the default state.
func TestChunkNBTSerializesBlocksWithProperties(t *testing.T) {
	t.Parallel()
	c := newChunk(0, 0)
	c.SetBiome(registry.MustID("minecraft:worldgen/biome", "minecraft:plains"))

	// Oak log, axis=x. The default is axis=y, so if the properties are lost
	// the block comes back as the default.
	logAxisX := uint16(136) // oak_log, axis=x
	c.SetBlock(0, worldMinY, 0, logAxisX)

	root, err := encodeChunkNBT(c)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("nbt.Decode: %v", err)
	}
	loaded, err := decodeChunkNBT(tag.(nbt.Compound))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	if got := loaded.BlockAt(0, worldMinY, 0); got != logAxisX {
		t.Errorf("log at (0,%d,0) = %d, want %d (axis=x, not the default axis=y)",
			got, worldMinY, logAxisX)
	}
}

// TestChunkNBTKeepsBlockEntities checks that the block entities of a chunk's
// on-disk NBT survive a load and a save: the ones this server models - a
// chest and its contents - become the live entities its windows read and are
// written back as the same chunk-format entries, and the kinds it does not
// model travel as the compounds they were read from, so a vanilla world does
// not lose its banners and spawners the first time it is saved here.
func TestChunkNBTKeepsBlockEntities(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String("minecraft:chest"),
		"x":  nbt.Int(0),
		"y":  nbt.Int(worldMinY),
		"z":  nbt.Int(0),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Byte(3),
				"id":    nbt.String("minecraft:diamond"),
				"count": nbt.Int(5),
			},
		}},
	}
	banner := nbt.Compound{
		"id": nbt.String("minecraft:banner"),
		"x":  nbt.Int(4),
		"y":  nbt.Int(worldMinY),
		"z":  nbt.Int(5),
		"patterns": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"code": nbt.String("brc"), "color": nbt.Int(0)},
		}},
	}
	root := nbt.Compound{
		"DataVersion":    nbt.Int(dataVersion),
		"xPos":           nbt.Int(0),
		"yPos":           nbt.Int(worldMinY / sectionSize),
		"zPos":           nbt.Int(0),
		"Status":         nbt.String("minecraft:full"),
		"sections":       nbt.List{Elem: nbt.TypeCompound},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{chest, banner}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}

	// The chest became a live entity with the diamonds where the file's own
	// slot numbering put them; the banner did not become anything, being
	// one of the kinds this server leaves as it found them.
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || be.Kind != KindChest {
		t.Fatalf("block entity at (%d,%d,0) = %v, want the chest", 0, worldMinY, be)
	}
	diamond, known := registry.ItemID("minecraft:diamond")
	if !known {
		t.Fatal("the item registry has no minecraft:diamond")
	}
	if got, want := len(be.Slots), int(containerSize[KindChest]); got != want {
		t.Errorf("chest holds %d slots, want %d", got, want)
	} else if !be.Slots[3].Equal(protocol.ItemStack{ItemID: diamond, Count: 5}) {
		t.Errorf("chest slot 3 = %+v, want 5 diamonds", be.Slots[3])
	}
	if got := len(loaded.carriedBlockEntities.Items); got != 1 {
		t.Fatalf("carried block entities = %d entries, want the banner alone", got)
	}

	// Re-encoding writes the chest back as a chunk-format entry - its
	// position, its id, and its Items with their Slot keys - and carries the
	// banner through as it was read.
	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	list, ok := nbt.Get[nbt.List](saved, "block_entities")
	if !ok || len(list.Items) != 2 {
		t.Fatalf("saved block entities = %v, want the chest and the banner", list)
	}
	savedChest, savedBanner := false, false
	for _, entry := range list.Items {
		compound, ok := entry.(nbt.CompoundTag)
		if !ok {
			continue
		}
		c := compound.Entries()
		switch id, _ := nbt.Get[nbt.String](c, "id"); kindOfID(string(id)) {
		case KindChest:
			savedChest = true
			items, ok := nbt.Get[nbt.List](c, "Items")
			if !ok || len(items.Items) != 1 {
				t.Fatalf("saved chest items = %v, want the one stack", items)
			}
			item, ok := nbt.CompoundView(items.Items[0])
			if !ok {
				t.Fatalf("saved chest item = %#v, want a compound", items.Items[0])
			}
			if slot, _ := nbt.Get[nbt.Byte](item, "Slot"); slot != 3 {
				t.Errorf("saved chest item slot = %d, want 3", slot)
			}
		case KindBanner:
			savedBanner = true
		}
	}
	if !savedChest || !savedBanner {
		t.Errorf("the chunk saved its chest (%v) and its banner (%v), wanted both", savedChest, savedBanner)
	}
}

// TestChunkNBTSignText checks the sign's own entry: a vanilla chunk
// file's sign becomes the live entity with both sides' text, colour and
// wax, and what a save writes back is the same shape vanilla's SignText
// codec writes - four plain lines a side, the colour name, the glow byte,
// the wax byte.
func TestChunkNBTSignText(t *testing.T) {
	t.Parallel()
	sign := nbt.Compound{
		"id":       nbt.String("minecraft:sign"),
		"x":        nbt.Int(4),
		"y":        nbt.Int(worldMinY),
		"z":        nbt.Int(5),
		"is_waxed": nbt.Byte(1),
		"front_text": nbt.Compound{
			"messages":         nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("one"), nbt.String("two"), nbt.String(""), nbt.String("")}},
			"color":            nbt.String("blue"),
			"has_glowing_text": nbt.Byte(1),
		},
		"back_text": nbt.Compound{
			"messages":         nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(""), nbt.String(""), nbt.String("back"), nbt.String("")}},
			"color":            nbt.String("black"),
			"has_glowing_text": nbt.Byte(0),
		},
	}
	root := nbt.Compound{
		"DataVersion":    nbt.Int(dataVersion),
		"xPos":           nbt.Int(0),
		"yPos":           nbt.Int(worldMinY / sectionSize),
		"zPos":           nbt.Int(0),
		"Status":         nbt.String("minecraft:full"),
		"sections":       nbt.List{Elem: nbt.TypeCompound},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{sign}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(4, worldMinY, 5)
	if be == nil || be.Kind != KindSign {
		t.Fatalf("block entity at (4,%d,5) = %v, want the sign", worldMinY, be)
	}
	if be.Waxed != true {
		t.Error("loaded sign is not waxed")
	}
	if be.SignFront.Messages != [4]string{"one", "two", "", ""} {
		t.Errorf("loaded front text = %q", be.SignFront.Messages)
	}
	if be.SignFront.Color != "blue" || !be.SignFront.Glowing {
		t.Errorf("loaded front colour/glow = %q %v, want blue and glowing", be.SignFront.Color, be.SignFront.Glowing)
	}
	if be.SignBack.Messages != [4]string{"", "", "back", ""} {
		t.Errorf("loaded back text = %q", be.SignBack.Messages)
	}
	if got := len(loaded.carriedBlockEntities.Items); got != 0 {
		t.Fatalf("carried block entities = %d entries, want none: the sign is live", got)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	list, ok := nbt.Get[nbt.List](saved, "block_entities")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("saved block entities = %v, want the sign alone", list)
	}
	entry := savedBlockEntity(t, saved, 0)
	if id, _ := nbt.Get[nbt.String](entry, "id"); id != "minecraft:sign" {
		t.Errorf("saved sign id = %q", id)
	}
	if waxed, _ := nbt.Get[nbt.Byte](entry, "is_waxed"); waxed != 1 {
		t.Error("saved sign is not waxed")
	}
	for _, side := range []struct {
		name string
		want SignText
	}{
		{"front_text", be.SignFront},
		{"back_text", be.SignBack},
	} {
		compound, ok := nbt.Get[nbt.Compound](entry, side.name)
		if !ok {
			t.Fatalf("saved sign has no %s: %#v", side.name, entry)
		}
		messages, ok := nbt.Get[nbt.List](compound, "messages")
		if !ok || len(messages.Items) != 4 {
			t.Fatalf("saved %s messages = %#v, want four", side.name, compound["messages"])
		}
		for i, want := range side.want.Messages {
			if got, ok := messages.Items[i].(nbt.String); !ok || string(got) != want {
				t.Errorf("saved %s message %d = %#v, want %q", side.name, i, messages.Items[i], want)
			}
		}
		if _, ok := nbt.Get[nbt.String](compound, "color"); !ok {
			t.Errorf("saved %s has no colour name", side.name)
		}
		glow, ok := nbt.Get[nbt.Byte](compound, "has_glowing_text")
		if !ok {
			t.Errorf("saved %s has no glow byte", side.name)
		} else if (glow != 0) != side.want.Glowing {
			t.Errorf("saved %s glow = %d, want %v", side.name, glow, side.want.Glowing)
		}
	}
}

// TestChunkNBTSignStyledText loads a sign whose lines a vanilla world saved
// as components - one styled, one translated, the plain ones as literals -
// and checks that a save writes each back whole rather than dropping the
// styling to the literal text.
func TestChunkNBTSignStyledText(t *testing.T) {
	t.Parallel()
	styled := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"text": nbt.String("red"), "color": nbt.String("red")},
		nbt.Compound{"": nbt.String("plain")},
		nbt.Compound{"translate": nbt.String("block.minecraft.stone")},
		nbt.Compound{"": nbt.String("")},
	}}
	sign := nbt.Compound{
		"id": nbt.String("minecraft:sign"),
		"x":  nbt.Int(4),
		"y":  nbt.Int(worldMinY),
		"z":  nbt.Int(5),
		"front_text": nbt.Compound{
			"messages":         styled,
			"color":            nbt.String("black"),
			"has_glowing_text": nbt.Byte(0),
		},
		"back_text": nbt.Compound{
			"messages":         nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(""), nbt.String(""), nbt.String(""), nbt.String("")}},
			"color":            nbt.String("black"),
			"has_glowing_text": nbt.Byte(0),
		},
	}
	root := nbt.Compound{
		"DataVersion":    nbt.Int(dataVersion),
		"xPos":           nbt.Int(0),
		"yPos":           nbt.Int(worldMinY / sectionSize),
		"zPos":           nbt.Int(0),
		"Status":         nbt.String("minecraft:full"),
		"sections":       nbt.List{Elem: nbt.TypeCompound},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{sign}},
	}
	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(4, worldMinY, 5)
	if be == nil || be.Kind != KindSign {
		t.Fatalf("block entity at (4,%d,5) = %v, want the sign", worldMinY, be)
	}
	// The literal text reads through; the translated line leaves the side
	// not plain text the editor may replace.
	if be.SignFront.Messages != [4]string{"red", "plain", "", ""} {
		t.Errorf("loaded front text = %q", be.SignFront.Messages)
	}
	if be.SignFront.Editable() {
		t.Error("a side with a translated line reads as editable, want it left alone")
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	entry := savedBlockEntity(t, saved, 0)
	front, _ := nbt.Get[nbt.Compound](entry, "front_text")
	messages, ok := nbt.Get[nbt.List](front, "messages")
	if !ok || len(messages.Items) != 4 {
		t.Fatalf("saved messages = %#v, want four", front["messages"])
	}
	if got, ok := messages.Items[0].(nbt.Compound); !ok || got["color"] != nbt.String("red") {
		t.Errorf("saved styled line = %#v, want the colour kept", messages.Items[0])
	}
	if got, ok := messages.Items[2].(nbt.Compound); !ok || got["translate"] != nbt.String("block.minecraft.stone") {
		t.Errorf("saved translated line = %#v, want the translation kept", messages.Items[2])
	}
	if got, ok := messages.Items[1].(nbt.Compound); !ok || got[""] != nbt.String("plain") {
		t.Errorf("saved plain line = %#v, want the literal compound vanilla writes beside a styled one", messages.Items[1])
	}
	// One list carries one element type, so the four compounds must encode.
	if _, err := nbt.EncodeNetwork(front); err != nil {
		t.Errorf("encode the saved front side: %v", err)
	}
}

// TestSignTextEditable checks which saved roots the editor may replace: a
// styled literal is still a literal, an empty line is allowed, while a
// translated or keybind root is not.
// vanilla: SignBlock.hasEditableText
func TestSignTextEditable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		message nbt.Tag
		want    bool
	}{
		{"string", nbt.String("hi"), true},
		{"empty line", nbt.String(""), true},
		{"literal", nbt.Compound{"text": nbt.String("hi")}, true},
		{"empty key", nbt.Compound{"": nbt.String("")}, true},
		{"styled literal", nbt.Compound{"text": nbt.String("red"), "color": nbt.String("red")}, true},
		{"literal with extra", nbt.Compound{"text": nbt.String("hi"), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("!")}}}, true},
		{"translated", nbt.Compound{"translate": nbt.String("block.minecraft.stone")}, false},
		{"keybind", nbt.Compound{"keybind": nbt.String("key.jump")}, false},
	}
	for _, tc := range tests {
		if got := decodedSignSide(tc.message).Editable(); got != tc.want {
			t.Errorf("%s: Editable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSignTextHasMessage checks what a dye or an ink sac reads as text on a
// side: a translated or keybind root says something even with no literal,
// while an empty or empty-literal line does not.
// vanilla: SignText.hasMessage
func TestSignTextHasMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		message nbt.Tag
		want    bool
	}{
		{"string", nbt.String("hi"), true},
		{"empty line", nbt.String(""), false},
		{"empty key", nbt.Compound{"": nbt.String("")}, false},
		{"styled empty literal", nbt.Compound{"text": nbt.String(""), "color": nbt.String("red")}, false},
		{"translated", nbt.Compound{"translate": nbt.String("block.minecraft.stone")}, true},
		{"keybind", nbt.Compound{"keybind": nbt.String("key.jump")}, true},
	}
	for _, tc := range tests {
		if got := decodedSignSide(tc.message).HasMessage(); got != tc.want {
			t.Errorf("%s: HasMessage = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// decodedSignSide builds one side of a sign whose first line is the component
// given, the other three empty, the way a chunk load reads them.
func decodedSignSide(message nbt.Tag) SignText {
	elem := nbt.TypeCompound
	empty := nbt.Tag(nbt.Compound{"": nbt.String("")})
	if _, ok := message.(nbt.String); ok {
		elem = nbt.TypeString
		empty = nbt.String("")
	}
	return decodeSignText(nbt.Compound{
		"messages":         nbt.List{Elem: elem, Items: []nbt.Tag{message, empty, empty, empty}},
		"color":            nbt.String("black"),
		"has_glowing_text": nbt.Byte(0),
	})
}

// TestChunkNBTFurnaceRecipesUsed checks the smelting history the furnace
// block entity saves: a compound of recipe id to finished-smelt count, the
// shape the vanilla server's own RECIPES_USED_CODEC writes - read back out
// of a file a vanilla server made, written back the way it would be, an
// empty one included, since the vanilla save does not leave the key out
// for a furnace that owes nothing.
func TestChunkNBTFurnaceRecipesUsed(t *testing.T) {
	t.Parallel()
	used := nbt.Compound{
		"minecraft:iron_ingot_from_smelting_raw_iron": nbt.Int(10),
		"minecraft:gold_ingot_from_smelting":          nbt.Int(3),
	}
	furnace := nbt.Compound{
		"id":                 nbt.String(KindFurnace.Name()),
		"x":                  nbt.Int(0),
		"y":                  nbt.Int(worldMinY),
		"z":                  nbt.Int(0),
		"Items":              nbt.List{Elem: nbt.TypeCompound},
		"cooking_time_spent": nbt.Int(0),
		"cooking_total_time": nbt.Int(0),
		"lit_time_remaining": nbt.Int(0),
		"lit_total_time":     nbt.Int(0),
		"RecipesUsed":        used,
	}
	root := nbt.Compound{
		"DataVersion":    nbt.Int(dataVersion),
		"xPos":           nbt.Int(0),
		"yPos":           nbt.Int(worldMinY / sectionSize),
		"zPos":           nbt.Int(0),
		"Status":         nbt.String("minecraft:full"),
		"sections":       nbt.List{Elem: nbt.TypeCompound},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{furnace}},
	}

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || be.Kind != KindFurnace {
		t.Fatalf("block entity at the position = %v, want the furnace", be)
	}
	if got := be.RecipesUsed.Count("minecraft:iron_ingot_from_smelting_raw_iron"); got != 10 {
		t.Errorf("recipes used for the raw iron = %d, want 10", got)
	}
	if got := be.RecipesUsed.Count("minecraft:gold_ingot_from_smelting"); got != 3 {
		t.Errorf("recipes used for the gold = %d, want 3", got)
	}
	if be.RecipesUsed.Len() != 2 {
		t.Errorf("recipes used has %d entries, want the file's own two", be.RecipesUsed.Len())
	}

	// Taking the history empties it, and the save writes the key back with
	// nothing in it - the shape a vanilla furnace that owes nothing has.
	if taken := be.TakeRecipesUsed(); len(taken) != 2 {
		t.Errorf("the history handed over has %d entries, want both", len(taken))
	}
	if be.RecipesUsed.Len() != 0 {
		t.Errorf("the history was not emptied by the taking")
	}
	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	list, ok := nbt.Get[nbt.List](saved, "block_entities")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("saved block entities = %v, want the furnace alone", list)
	}
	entry := savedBlockEntity(t, saved, 0)
	savedUsed, ok := nbt.Get[nbt.Compound](entry, "RecipesUsed")
	if !ok {
		t.Fatal("the saved furnace carries no RecipesUsed, want the empty compound the vanilla save writes")
	}
	if len(savedUsed) != 0 {
		t.Errorf("the saved furnace owes %v, want nothing", savedUsed)
	}
}

// vanillaChunkWith builds a chunk root around the given block entity entries,
// the shape a vanilla server saves a chunk with: the flat chunk's own fields
// and the entries under block_entities.
func vanillaChunkWith(entries ...nbt.Tag) nbt.Compound {
	return nbt.Compound{
		"DataVersion":    nbt.Int(dataVersion),
		"xPos":           nbt.Int(0),
		"yPos":           nbt.Int(worldMinY / sectionSize),
		"zPos":           nbt.Int(0),
		"Status":         nbt.String("minecraft:full"),
		"sections":       nbt.List{Elem: nbt.TypeCompound},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: entries},
	}
}

// savedBlockEntity reads one entry of a saved chunk's block_entities list.
func savedBlockEntity(t *testing.T, saved nbt.Compound, i int) nbt.Compound {
	t.Helper()
	list, ok := nbt.Get[nbt.List](saved, "block_entities")
	if !ok || len(list.Items) <= i {
		t.Fatalf("saved block entities = %v, want at least %d", list, i+1)
	}
	entry, ok := list.Items[i].(nbt.CompoundTag)
	if !ok {
		t.Fatalf("saved block entity %d = %#v, want a compound", i, list.Items[i])
	}
	return entry.Entries()
}

// TestChunkNBTReadsSlotAndCountAtAnyWidth checks a stored stack whose fields a
// vanilla file may have written at another width: an int Slot, which this
// server's own writer writes as a byte, and a byte count. Both read into the
// stack, and the save writes them back in the server's own shape.
func TestChunkNBTReadsSlotAndCountAtAnyWidth(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String(KindChest.Name()),
		"x":  nbt.Int(0),
		"y":  nbt.Int(worldMinY),
		"z":  nbt.Int(0),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Int(3),
				"id":    nbt.String("minecraft:diamond"),
				"count": nbt.Byte(5),
			},
		}},
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(chest))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || be.Kind != KindChest {
		t.Fatalf("block entity at the position = %v, want the chest", be)
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no minecraft:diamond")
	}
	if got := be.Slots[3]; !got.Equal(protocol.ItemStack{ItemID: diamond, Count: 5}) {
		t.Fatalf("chest slot 3 = %+v, want five diamonds", got)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	items, ok := nbt.Get[nbt.List](savedBlockEntity(t, saved, 0), "Items")
	if !ok || len(items.Items) != 1 {
		t.Fatalf("saved Items = %v, want the one stack", items)
	}
	item, ok := nbt.CompoundView(items.Items[0])
	if !ok {
		t.Fatalf("saved stack = %#v, want a compound", items.Items[0])
	}
	if slot, ok := nbt.Get[nbt.Byte](item, "Slot"); !ok || slot != 3 {
		t.Errorf("saved Slot = %v, want the byte 3", item["Slot"])
	}
	if count, ok := nbt.Get[nbt.Int](item, "count"); !ok || count != 5 {
		t.Errorf("saved count = %v, want the int 5", item["count"])
	}
}

// TestChunkNBTSavesFurnaceTimesAsInt checks the furnace's four progress
// times: 26.3 writes them as ints, and a file of the older shorts is read and
// saved back as ints.
func TestChunkNBTSavesFurnaceTimesAsInt(t *testing.T) {
	t.Parallel()
	furnace := nbt.Compound{
		"id":                 nbt.String(KindFurnace.Name()),
		"x":                  nbt.Int(0),
		"y":                  nbt.Int(worldMinY),
		"z":                  nbt.Int(0),
		"Items":              nbt.List{Elem: nbt.TypeCompound},
		"cooking_time_spent": nbt.Short(15),
		"cooking_total_time": nbt.Short(200),
		"lit_time_remaining": nbt.Short(80),
		"lit_total_time":     nbt.Short(200),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(furnace))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || !IsCooking(be.Kind) {
		t.Fatalf("block entity at the position = %v, want the furnace", be)
	}
	if be.CookingSpent != 15 || be.CookingTotal != 200 || be.LitRemaining != 80 || be.LitTotal != 200 {
		t.Errorf("loaded furnace times = (%d, %d, %d, %d), want (15, 200, 80, 200)",
			be.CookingSpent, be.CookingTotal, be.LitRemaining, be.LitTotal)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	entry := savedBlockEntity(t, saved, 0)
	for _, field := range []struct {
		name string
		want nbt.Int
	}{
		{"cooking_time_spent", 15},
		{"cooking_total_time", 200},
		{"lit_time_remaining", 80},
		{"lit_total_time", 200},
	} {
		if got, ok := nbt.Get[nbt.Int](entry, field.name); !ok || got != field.want {
			t.Errorf("saved %s = %v, want the int %d", field.name, entry[field.name], field.want)
		}
	}
}

// TestChunkNBTKeepsLootTableAndCustomName checks the two fields a
// RandomizableContainerBlockEntity keeps beside its items: the loot table and
// its seed a structure chest was spawned with, and the custom name, both kept
// through a load and a save rather than dropped for the modelled kind. A
// furnace, which holds no table, drops one.
func TestChunkNBTKeepsLootTableAndCustomName(t *testing.T) {
	t.Parallel()
	const name = `{"text":"Loot"}`
	chest := nbt.Compound{
		"id":            nbt.String(KindChest.Name()),
		"x":             nbt.Int(0),
		"y":             nbt.Int(worldMinY),
		"z":             nbt.Int(0),
		"Items":         nbt.List{Elem: nbt.TypeCompound},
		"LootTable":     nbt.String("minecraft:chests/simple_dungeon"),
		"LootTableSeed": nbt.Long(7),
		"CustomName":    nbt.String(name),
	}
	furnace := nbt.Compound{
		"id":        nbt.String(KindFurnace.Name()),
		"x":         nbt.Int(4),
		"y":         nbt.Int(worldMinY),
		"z":         nbt.Int(4),
		"Items":     nbt.List{Elem: nbt.TypeCompound},
		"LootTable": nbt.String("minecraft:chests/simple_dungeon"),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(chest, furnace))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil {
		t.Fatal("the chest did not become a live entity")
	}
	if be.LootTable != "minecraft:chests/simple_dungeon" {
		t.Errorf("chest LootTable = %q, want the table", be.LootTable)
	}
	if be.LootTableSeed != 7 {
		t.Errorf("chest LootTableSeed = %d, want 7", be.LootTableSeed)
	}
	if be.CustomName == nil {
		t.Error("the chest's CustomName was dropped")
	}
	if f := loaded.blockEntity(4, worldMinY, 4); f == nil || f.LootTable != "" {
		t.Errorf("the furnace kept a loot table, want it dropped: %v", f)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	entry := savedBlockEntity(t, saved, 0)
	if got, ok := nbt.Get[nbt.String](entry, "LootTable"); !ok || got != "minecraft:chests/simple_dungeon" {
		t.Errorf("saved LootTable = %v, want the table", entry["LootTable"])
	}
	if got, ok := nbt.Get[nbt.Long](entry, "LootTableSeed"); !ok || got != 7 {
		t.Errorf("saved LootTableSeed = %v, want the long 7", entry["LootTableSeed"])
	}
	if got, ok := nbt.Get[nbt.String](entry, "CustomName"); !ok || got != name {
		t.Errorf("saved CustomName = %v, want %q", entry["CustomName"], name)
	}
}

// TestChunkNBTLootTableAndItemsDoNotShareASave checks the one-of-two rule a
// RandomizableContainerBlockEntity follows: a file naming a table loads no
// list of its own, and a save holding a stack drops the table for the items,
// so the two are never written together.
func TestChunkNBTLootTableAndItemsDoNotShareASave(t *testing.T) {
	t.Parallel()
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no minecraft:diamond")
	}
	chest := nbt.Compound{
		"id":        nbt.String(KindChest.Name()),
		"x":         nbt.Int(0),
		"y":         nbt.Int(worldMinY),
		"z":         nbt.Int(0),
		"LootTable": nbt.String("minecraft:chests/simple_dungeon"),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Byte(5),
				"id":    nbt.String("minecraft:diamond"),
				"count": nbt.Int(1),
			},
		}},
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(chest))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil {
		t.Fatal("the chest did not become a live entity")
	}
	if be.LootTable != "minecraft:chests/simple_dungeon" {
		t.Errorf("chest LootTable = %q, want the table", be.LootTable)
	}
	if got := be.Slots[5]; !got.Empty() {
		t.Errorf("a file naming a table loaded the item %+v beside it", got)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	entry := savedBlockEntity(t, saved, 0)
	if items, ok := entry["Items"]; ok {
		t.Errorf("saved an item list beside the table: %v", items)
	}
	if got, ok := nbt.Get[nbt.String](entry, "LootTable"); !ok || got != "minecraft:chests/simple_dungeon" {
		t.Errorf("saved LootTable = %v, want the table", entry["LootTable"])
	}

	// A stack in the container replaces the table on the next save.
	be.Slots[5] = protocol.ItemStack{ItemID: diamond, Count: 1}
	saved, err = encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	entry = savedBlockEntity(t, saved, 0)
	if table, ok := entry["LootTable"]; ok {
		t.Errorf("kept the table once a stack was in the container: %v", table)
	}
	items, ok := nbt.Get[nbt.List](entry, "Items")
	if !ok || len(items.Items) != 1 {
		t.Fatalf("saved Items = %v, want the one stack", items)
	}
	item, ok := nbt.CompoundView(items.Items[0])
	if !ok {
		t.Fatalf("saved stack = %#v, want a compound", items.Items[0])
	}
	if slot, ok := nbt.Get[nbt.Byte](item, "Slot"); !ok || slot != 5 {
		t.Errorf("saved Slot = %v, want the byte 5", item["Slot"])
	}
}

// TestChunkNBTReadsLootTableSeedAtAnyWidth checks a seed written as an int:
// getLongOr takes any numeric tag, so a file whose seed is an int keeps it
// rather than reading as nought.
func TestChunkNBTReadsLootTableSeedAtAnyWidth(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id":            nbt.String(KindChest.Name()),
		"x":             nbt.Int(0),
		"y":             nbt.Int(worldMinY),
		"z":             nbt.Int(0),
		"LootTable":     nbt.String("minecraft:chests/simple_dungeon"),
		"LootTableSeed": nbt.Int(7),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(chest))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil {
		t.Fatal("the chest did not become a live entity")
	}
	if be.LootTableSeed != 7 {
		t.Errorf("chest LootTableSeed = %d, want the int seed 7", be.LootTableSeed)
	}
}

// TestChunkNBTCrafterKeepsLootTable checks that the crafter, a
// RandomizableContainerBlockEntity, keeps the table a file gives it.
func TestChunkNBTCrafterKeepsLootTable(t *testing.T) {
	t.Parallel()
	crafter := nbt.Compound{
		"id":        nbt.String(KindCrafter.Name()),
		"x":         nbt.Int(0),
		"y":         nbt.Int(worldMinY),
		"z":         nbt.Int(0),
		"LootTable": nbt.String("minecraft:chests/simple_dungeon"),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(crafter))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil {
		t.Fatal("the crafter did not become a live entity")
	}
	if be.Kind != KindCrafter {
		t.Fatalf("block entity = %q, want a crafter", be.Kind)
	}
	if be.LootTable != "minecraft:chests/simple_dungeon" {
		t.Errorf("crafter LootTable = %q, want the table", be.LootTable)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	if got, ok := nbt.Get[nbt.String](savedBlockEntity(t, saved, 0), "LootTable"); !ok || got != "minecraft:chests/simple_dungeon" {
		t.Errorf("saved LootTable = %v, want the table", got)
	}
}

// TestChunkNBTKeepsWideFurnaceTimes checks that a furnace time written as an
// int past a short is read whole.
func TestChunkNBTKeepsWideFurnaceTimes(t *testing.T) {
	t.Parallel()
	furnace := nbt.Compound{
		"id":                 nbt.String(KindFurnace.Name()),
		"x":                  nbt.Int(0),
		"y":                  nbt.Int(worldMinY),
		"z":                  nbt.Int(0),
		"cooking_time_spent": nbt.Int(70000),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(furnace))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil {
		t.Fatal("the furnace did not become a live entity")
	}
	if be.CookingSpent != 70000 {
		t.Errorf("cooking_time_spent = %d, want 70000", be.CookingSpent)
	}
}

// TestChunkNBTFurnaceTimesFloorFloats checks a furnace time a hand-edited file
// wrote as a float or a double: the vanilla short floors it, so -1.5 is the
// short -2 rather than being truncated toward zero.
func TestChunkNBTFurnaceTimesFloorFloats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tag  nbt.Tag
	}{
		{"float", nbt.Float(-1.5)},
		{"double", nbt.Double(-1.5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			be := newBlockEntity(KindFurnace, 0, 0, 0)
			be.loadFrom(nbt.Compound{"cooking_time_spent": tc.tag})
			if be.CookingSpent != -2 {
				t.Errorf("cooking_time_spent loaded as %d, want the short -2", be.CookingSpent)
			}
		})
	}
}

// TestChunkNBTNarrowsSlotToByte checks a stored stack whose Slot a file wrote
// wider than a byte: the codec reads an unsigned byte, so an int 259 is the
// slot 3 rather than a slot past the end dropped on load.
func TestChunkNBTNarrowsSlotToByte(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String(KindChest.Name()),
		"x":  nbt.Int(0),
		"y":  nbt.Int(worldMinY),
		"z":  nbt.Int(0),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Int(259),
				"id":    nbt.String("minecraft:diamond"),
				"count": nbt.Byte(5),
			},
		}},
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(chest))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || be.Kind != KindChest {
		t.Fatalf("block entity at the position = %v, want the chest", be)
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no minecraft:diamond")
	}
	if got := be.Slots[3]; !got.Equal(protocol.ItemStack{ItemID: diamond, Count: 5}) {
		t.Fatalf("chest slot 3 = %+v, want five diamonds", got)
	}
}

// TestChunkNBTKeepsUnmodelledRootTags checks that the root tags this server
// does not model survive a load and a save: a vanilla chunk's structure starts,
// PostProcessing, carving_mask, UpgradeData and blending_data go out as they
// came in, so a vanilla world does not lose its villages and strongholds the
// first time it is saved here. The tags the server writes itself - LastUpdate
// and InhabitedTime among them - the tick lists, and isLightOn are not kept
// as opaque copies; the age rides out as what the chunk loaded with.
func TestChunkNBTKeepsUnmodelledRootTags(t *testing.T) {
	t.Parallel()
	structures := nbt.Compound{
		"References": nbt.Compound{},
		"Starts": nbt.Compound{
			"minecraft:village_plains": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{
					"id":     nbt.String("minecraft:village_plains"),
					"ChunkX": nbt.Int(0),
					"ChunkZ": nbt.Int(0),
				},
			}},
		},
	}
	postProcessing := nbt.List{Elem: nbt.TypeList, Items: []nbt.Tag{
		nbt.List{Elem: nbt.TypeShort, Items: []nbt.Tag{nbt.Short(0), nbt.Short(0)}},
	}}
	upgradeData := nbt.Compound{
		"neighbor_block_ticks": nbt.List{Elem: nbt.TypeCompound},
	}
	blendingData := nbt.Compound{"min_section": nbt.Int(-4), "max_section": nbt.Int(20)}

	root := vanillaChunkWith()
	root["structures"] = structures
	root["PostProcessing"] = postProcessing
	root["carving_mask"] = nbt.LongArray{1, 2, 3}
	root["UpgradeData"] = upgradeData
	root["blending_data"] = blendingData
	// The tags the server leaves alone, none of them coming back as a carried
	// copy; LastUpdate and InhabitedTime are modelled, and go back out from
	// the chunk's own fields.
	root["isLightOn"] = nbt.Byte(1)
	root["InhabitedTime"] = nbt.Long(4096)
	root["block_ticks"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"i": nbt.String("minecraft:water"),
			"x": nbt.Int(0), "y": nbt.Int(0), "z": nbt.Int(0),
			"t": nbt.Int(5), "p": nbt.Int(0),
		},
	}}
	root["fluid_ticks"] = nbt.List{Elem: nbt.TypeCompound}
	root["LastUpdate"] = nbt.Long(1)

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}

	for name, want := range map[string]nbt.Tag{
		"structures":     structures,
		"PostProcessing": postProcessing,
		"carving_mask":   nbt.LongArray{1, 2, 3},
		"UpgradeData":    upgradeData,
		"blending_data":  blendingData,
	} {
		got, ok := saved[name]
		if !ok {
			t.Errorf("the saved chunk dropped %s", name)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("saved %s = %#v, want %#v", name, got, want)
		}
	}
	if _, ok := saved["isLightOn"]; ok {
		t.Error("the saved chunk carried isLightOn, want it left out")
	}
	ticks, ok := nbt.Get[nbt.List](saved, "block_ticks")
	if !ok || len(ticks.Items) != 1 {
		t.Fatalf("saved block_ticks = %v, %v; want the one tick the chunk loaded with", ticks, ok)
	}
	if got := ticks.Items[0].(nbt.Compound); !reflect.DeepEqual(got, root["block_ticks"].(nbt.List).Items[0]) {
		t.Errorf("saved block tick = %#v, want %#v", got, root["block_ticks"].(nbt.List).Items[0])
	}
	if _, ok := saved["fluid_ticks"]; ok {
		t.Error("the saved chunk wrote an empty fluid_ticks")
	}
	if got, ok := nbt.Get[nbt.Long](saved, "InhabitedTime"); !ok || got != 4096 {
		t.Errorf("saved InhabitedTime = %v, %v; want the 4096 the chunk loaded with", got, ok)
	}
	if got, ok := nbt.Get[nbt.Long](saved, "LastUpdate"); !ok {
		t.Error("the saved chunk has no LastUpdate")
	} else if got != 1 {
		t.Errorf("saved LastUpdate = %d, want the 1 the chunk was loaded with", got)
	}
}

// TestChunkNBTKeepsTheClaimedDataVersion checks the DataVersion a chunk's file
// claimed rides through a save: the block entities and entities it carries
// unread hold the shape of the version that wrote them, and a current stamp
// over them would tell the game they need none of its upgrades, so it keeps
// claiming what it is and a generated chunk, which claims nothing, takes the
// stamp of the version writing it — level.dat's rule.
func TestChunkNBTKeepsTheClaimedDataVersion(t *testing.T) {
	t.Parallel()
	// 1.20.4, whose items were the last to carry Count and tag; the chest
	// keeps the stack unread and rides it through.
	oldItems := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"Slot":  nbt.Byte(2),
			"id":    nbt.String("minecraft:diamond"),
			"Count": nbt.Byte(5),
		},
	}}
	chunkRoot := func(claim nbt.Tag) nbt.Compound {
		root := vanillaChunkWith(nbt.Compound{
			"id":    nbt.String(KindChest.Name()),
			"x":     nbt.Int(0),
			"y":     nbt.Int(worldMinY),
			"z":     nbt.Int(0),
			"Items": oldItems,
		})
		if claim != nil {
			root["DataVersion"] = claim
		} else {
			delete(root, "DataVersion")
		}
		return root
	}

	for _, tc := range []struct {
		name  string
		claim nbt.Tag
		want  int32
	}{
		{"a chunk without a claim, which a generated chunk is", nil, dataVersion},
		{"a 1.20.4 chunk", nbt.Int(3700), 3700},
		{"a current chunk", nbt.Int(dataVersion), dataVersion},
		{"a chunk of a newer version", nbt.Int(5000), 5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := decodeChunkNBT(chunkRoot(tc.claim))
			if err != nil {
				t.Fatalf("decodeChunkNBT: %v", err)
			}
			saved, err := encodeChunkNBT(loaded)
			if err != nil {
				t.Fatalf("encodeChunkNBT: %v", err)
			}
			if version, ok := nbt.Get[nbt.Int](saved, "DataVersion"); !ok || int32(version) != tc.want {
				t.Errorf("saved DataVersion = %v, %v; want %v", version, ok, tc.want)
			}
			if items, ok := nbt.Get[nbt.List](savedBlockEntity(t, saved, 0), "Items"); !ok || !reflect.DeepEqual(items, oldItems) {
				t.Errorf("saved chest Items =\n%v\nwant\n%v", items, oldItems)
			}

			// The claim outlives further saves of the same chunk.
			reloaded, err := decodeChunkNBT(saved)
			if err != nil {
				t.Fatalf("decodeChunkNBT: %v", err)
			}
			resaved, err := encodeChunkNBT(reloaded)
			if err != nil {
				t.Fatalf("encodeChunkNBT: %v", err)
			}
			if version, ok := nbt.Get[nbt.Int](resaved, "DataVersion"); !ok || int32(version) != tc.want {
				t.Errorf("resaved DataVersion = %v, %v; want %v", version, ok, tc.want)
			}
		})
	}
}

// TestChunkNBTLegacyFurnaceTimes checks a chunk claiming less than
// BlockEntityFurnaceBurnTimeFix (4181): its furnace's progress is read from
// the old CookTime/CookTimeTotal/BurnTime names and saved back under them, so
// the file's own fixer still rebuilds the cooking_time_spent/
// cooking_total_time/lit_time_remaining vanilla reads.
func TestChunkNBTLegacyFurnaceTimes(t *testing.T) {
	t.Parallel()
	furnace := nbt.Compound{
		"id":            nbt.String(KindFurnace.Name()),
		"x":             nbt.Int(0),
		"y":             nbt.Int(worldMinY),
		"z":             nbt.Int(0),
		"CookTime":      nbt.Int(15),
		"CookTimeTotal": nbt.Int(200),
		"BurnTime":      nbt.Int(80),
	}
	root := vanillaChunkWith(furnace)
	root["DataVersion"] = nbt.Int(3700)

	loaded, err := decodeChunkNBT(root)
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	be := loaded.blockEntity(0, worldMinY, 0)
	if be == nil || be.Kind != KindFurnace {
		t.Fatalf("block entity at the position = %v, want the furnace", be)
	}
	if be.CookingSpent != 15 || be.CookingTotal != 200 || be.LitRemaining != 80 {
		t.Errorf("loaded furnace times = (%d, %d, %d), want (15, 200, 80)",
			be.CookingSpent, be.CookingTotal, be.LitRemaining)
	}

	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	if version, ok := nbt.Get[nbt.Int](saved, "DataVersion"); !ok || int32(version) != 3700 {
		t.Errorf("saved DataVersion = %v, %v; want 3700", version, ok)
	}
	entry := savedBlockEntity(t, saved, 0)
	for field, want := range map[string]nbt.Tag{
		"CookTime":      nbt.Int(15),
		"CookTimeTotal": nbt.Int(200),
		"BurnTime":      nbt.Int(80),
	} {
		if got, ok := entry[field]; !ok || got != want {
			t.Errorf("saved furnace %s = %v, %v; want %v", field, got, ok, want)
		}
	}
	for _, field := range []string{"cooking_time_spent", "cooking_total_time", "lit_time_remaining", "lit_total_time"} {
		if _, ok := entry[field]; ok {
			t.Errorf("saved furnace still carries %s", field)
		}
	}
}

func TestLoadChunkStoredAtWrongPositionIsRelocated(t *testing.T) {
	dir := t.TempDir()
	root, err := encodeChunkNBT(newChunk(0, 0))
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	root["block_entities"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:chest"), "x": nbt.Int(1), "y": nbt.Int(64), "z": nbt.Int(1)},
	}}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("nbt.Encode: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dimensionDir(dir, DimensionName), "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	rf, err := openRegion(regionFilePath(dimensionDir(dir, DimensionName), 1, 0), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	// Index 0 of r.1.0 is chunk (32,0); the record inside claims (0,0).
	if err := rf.writeChunk(ChunkPos{32, 0}, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	if err := rf.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.loadChunk(ChunkPos{32, 0})
	if err != nil || c == nil {
		t.Fatalf("loadChunk = %v, %v", c, err)
	}
	if c.X() != 32 || c.Z() != 0 {
		t.Fatalf("chunk reports (%d,%d), want (32,0)", c.X(), c.Z())
	}
	if be := c.blockEntity(32*16+1, 64, 1); be == nil {
		t.Fatalf("chest not relocated into chunk (32,0)")
	}
}

func sectionTallies(s *section) [8]int {
	return [8]int{s.ticking, s.emitting, s.listening, s.rods, s.catalysts, s.eggs, int(s.touches.Load()), s.colliding}
}

func checkSectionTallies(t *testing.T, s *section) {
	t.Helper()
	checkRandomTickCount(t, s)
	var copy section
	copy.fill = s.fill
	copy.blocks.Store(s.blocks.Load())
	copy.recalcCounts()
	if got, want := sectionTallies(s), sectionTallies(&copy); got != want {
		t.Fatalf("section tallies = %v, recount = %v", got, want)
	}
}

func TestChunkNBTPaletteCounts(t *testing.T) {
	t.Parallel()
	states := []uint16{
		registry.BlockStates["minecraft:glowstone"], stoneState,
		registry.BlockStates["minecraft:oak_leaves"],
		registry.BlockStates["minecraft:glowstone"],
		registry.BlockStates["minecraft:sculk_sensor"],
		registry.BlockStates["minecraft:lightning_rod"],
		registry.BlockStates["minecraft:sculk_catalyst"],
		registry.BlockStates["minecraft:stone_pressure_plate"],
	}
	for _, size := range []int{1, 8, 17} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			palette := nbt.List{Elem: nbt.TypeCompound}
			for i := range size {
				palette.Items = append(palette.Items, blockPaletteEntry(states[i%len(states)]))
			}
			indices := make([]uint16, blocksPerSection)
			for i := range indices {
				indices[i] = uint16(i % size)
			}
			// An out-of-palette index must become air, including its tallies.
			if size > 1 {
				indices[0], indices[blocksPerSection-1] = uint16(size), uint16(size)
			}
			var s section
			s.ticking, s.emitting, s.listening, s.rods, s.catalysts, s.eggs = 42, 42, 42, 42, 42, 42
			s.touches.Store(42)
			if err := decodeSectionBlocks(&s, nbt.Compound{
				"palette": palette,
				"data":    packLongs(indices, max(minIndirectBits, bits.Len(uint(size-1)))),
			}); err != nil {
				t.Fatal(err)
			}
			for i, idx := range indices {
				want := uint16(airState)
				if int(idx) < size {
					want = states[int(idx)%len(states)]
				}
				if got := s.at(i); got != want {
					t.Fatalf("block %d = %d, want %d", i, got, want)
				}
			}
			checkSectionTallies(t, &s)
		})
	}
}

func TestChunkNBTGeneratedOverworldSaveLoad(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewOverworldGenerator(5))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}
	original := newChunk(pos.X, pos.Z)
	w.gen.Generate(original)
	if saved, err := w.saveChunk(pos, original); err != nil || !saved {
		t.Fatalf("saveChunk = %v, %v", saved, err)
	}
	loaded, err := w.loadChunk(pos)
	if err != nil || loaded == nil {
		t.Fatalf("loadChunk = %v, %v", loaded, err)
	}
	for si := range original.sections {
		want, got := &original.sections[si], &loaded.sections[si]
		for i := range blocksPerSection {
			if got.at(i) != want.at(i) {
				t.Fatalf("section %d block %d = %d, want %d", si, i, got.at(i), want.at(i))
			}
		}
		if sectionTallies(got) != sectionTallies(want) {
			t.Fatalf("section %d tallies = %v, original = %v", si, sectionTallies(got), sectionTallies(want))
		}
		checkSectionTallies(t, got)
	}
}

// TestChunkNBTKeepsPotSherdTemplatesAndBrewIntsThroughASave loads a decorated
// pot with an absent side and a side carrying components, and a brewing stand
// whose brew time and fuel are past a short and a byte, and checks a save
// writes them back unchanged.
// vanilla: PotDecorations.CODEC, BrewingStandBlockEntity.saveAdditional
func TestChunkNBTKeepsPotSherdTemplatesAndBrewIntsThroughASave(t *testing.T) {
	t.Parallel()
	left := nbt.Compound{
		"id":    nbt.String("minecraft:arms_up_pottery_sherd"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:custom_name": nbt.String("Marked"),
		},
	}
	pot := nbt.Compound{
		"id": nbt.String(KindDecoratedPot.Name()), "x": nbt.Int(0), "y": nbt.Int(worldMinY), "z": nbt.Int(0),
		"sherds": nbt.Compound{"left": left},
	}
	stand := nbt.Compound{
		"id": nbt.String(KindBrewingStand.Name()), "x": nbt.Int(1), "y": nbt.Int(worldMinY), "z": nbt.Int(0),
		"Items":      nbt.List{Elem: nbt.TypeCompound},
		"BrewTime":   nbt.Int(40000),
		"Fuel":       nbt.Int(200),
		"total_fuel": nbt.Int(200),
	}
	loaded, err := decodeChunkNBT(vanillaChunkWith(pot, stand))
	if err != nil {
		t.Fatalf("decodeChunkNBT: %v", err)
	}
	saved, err := encodeChunkNBT(loaded)
	if err != nil {
		t.Fatalf("encodeChunkNBT: %v", err)
	}
	sherds, _ := nbt.Get[nbt.Compound](savedBlockEntity(t, saved, 0), "sherds")
	if _, has := sherds["back"]; has || sherds["right"] != nil || sherds["front"] != nil || len(sherds) != 1 {
		t.Errorf("saved sherds = %v, want the left side alone", sherds)
	}
	savedLeft, _ := nbt.Get[nbt.Compound](sherds, "left")
	if savedLeft["id"] != left["id"] || savedLeft["components"] == nil {
		t.Errorf("saved left sherd = %v, want the sherd with its components", savedLeft)
	}
	brew := savedBlockEntity(t, saved, 1)
	if got, _ := nbt.Get[nbt.Int](brew, "BrewTime"); got != 40000 {
		t.Errorf("saved BrewTime = %v, want the int 40000", brew["BrewTime"])
	}
	if got, _ := nbt.Get[nbt.Int](brew, "Fuel"); got != 200 {
		t.Errorf("saved Fuel = %v, want the int 200", brew["Fuel"])
	}
}

// TestFixLegacyPaletteRenamesByVersion checks each rename applies only below
// its data version and keeps the entry's key spelling.
func TestFixLegacyPaletteRenamesByVersion(t *testing.T) {
	entries := func() nbt.List {
		return nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("test_block"), "properties": nbt.Compound{"test_block_mode": nbt.String("log")}},
			nbt.Compound{"Name": nbt.String("minecraft:creaking_heart"), "Properties": nbt.Compound{"active": nbt.String("false")}},
			nbt.Compound{"Name": nbt.String("minecraft:chain")},
		}}
	}
	old := entries()
	fixLegacyPalette(old, 4189)
	want := []nbt.Tag{
		nbt.Compound{"id": nbt.String("test_block"), "properties": nbt.Compound{"mode": nbt.String("log")}},
		nbt.Compound{"Name": nbt.String("minecraft:creaking_heart"), "Properties": nbt.Compound{"creaking_heart_state": nbt.String("uprooted")}},
		nbt.Compound{"Name": nbt.String("minecraft:iron_chain")},
	}
	if !reflect.DeepEqual(old.Items, want) {
		t.Errorf("v4189 palette = %v, want %v", old.Items, want)
	}
	// Between the property renames and the chain rename only the chain changes.
	mid := entries()
	fixLegacyPalette(mid, 4440)
	want = append(entries().Items[:2], nbt.Compound{"Name": nbt.String("minecraft:iron_chain")})
	if !reflect.DeepEqual(mid.Items, want) {
		t.Errorf("v4440 palette = %v, want %v", mid.Items, want)
	}
}

func TestChunkNBTUniformAndInvalidTickStates(t *testing.T) {
	leaf := registry.BlockOakLeaves.DefaultState()
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{blockPaletteEntry(leaf), blockPaletteEntry(airState)}}
	for _, mode := range []string{"uniform", "explicit-uniform", "mixed", "invalid-index"} {
		t.Run(mode, func(t *testing.T) {
			var s section
			s.setAll(airState)
			s.set(4095, leaf, false)
			tag := nbt.Compound{"palette": palette}
			if mode != "uniform" {
				indices := make([]uint16, blocksPerSection)
				if mode == "mixed" {
					indices[0], indices[31], indices[4095] = 1, 1, 1
				}
				if mode == "invalid-index" {
					indices[0], indices[31], indices[4095] = 15, 15, 15
				}
				tag["data"] = packLongs(indices, 4)
			}
			if err := decodeSectionBlocks(&s, tag); err != nil {
				t.Fatal(err)
			}
			checkSectionTallies(t, &s)
			if s.at(1) != leaf {
				t.Fatal("valid index changed")
			}
			if mode == "invalid-index" && (s.at(0) != airState || s.at(31) != airState || s.at(4095) != airState) {
				t.Fatal("invalid indices must read air")
			}
			if err := decodeSectionBlocks(&s, nbt.Compound{"palette": palette, "data": nbt.LongArray{0}}); err == nil {
				t.Fatal("accepted short data")
			}
			checkRandomTickCount(t, &s)
		})
	}
}
