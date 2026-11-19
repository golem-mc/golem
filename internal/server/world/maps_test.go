package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// writeMapData writes the named map's saved data file under dir, the gzipped
// {DataVersion, data} compound the game stores, with the fields the test
// wants in the data compound.
func writeMapData(t *testing.T, dir, name string, data nbt.Compound) {
	t.Helper()
	path := filepath.Join(dir, "data", "minecraft", "maps", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the maps folder: %v", err)
	}
	encoded, err := nbt.Encode("", nbt.Compound{"data": data, "DataVersion": nbt.Int(4903)})
	if err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatalf("compress %s: %v", name, err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compress %s: %v", name, err)
	}
	if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestMapsReadTheSavedData checks the codec's shape: the scale clamped to
// its range and absent fields read at their default, the colours taken only
// whole — a file with fewer or more of them keeps the blank face vanilla's
// own field default would.
// vanilla: MapItemSavedData.CODEC
func TestMapsReadTheSavedData(t *testing.T) {
	dir := t.TempDir()
	colours := make([]byte, MapSide*MapSide)
	for i := range colours {
		colours[i] = byte(i)
	}
	writeMapData(t, dir, "0.dat", nbt.Compound{
		"dimension":         nbt.String("minecraft:overworld"),
		"xCenter":           nbt.Int(64),
		"zCenter":           nbt.Int(64),
		"scale":             nbt.Byte(1),
		"colors":            nbt.ByteArray(colours),
		"locked":            nbt.Byte(1),
		"trackingPosition":  nbt.Byte(0),
		"unlimitedTracking": nbt.Byte(1),
		"banners":           nbt.List{},
		"frames":            nbt.List{},
	})

	maps := NewMaps(dir)
	data, ok := maps.Get(0)
	if !ok {
		t.Fatal("map 0 did not read, want the file the test wrote")
	}
	if data.Scale != 1 {
		t.Errorf("the scale = %d, want 1", data.Scale)
	}
	if !data.Locked {
		t.Error("the locked flag = false, want the file's true")
	}
	if want := [MapSide * MapSide]byte(colours); data.Colors != want {
		t.Errorf("the colours start with %v..., want the file's %v...", data.Colors[:8], colours[:8])
	}

	// A second read answers the copy it kept, and the file's being
	// rewritten behind it changes nothing: the cache is the one read.
	if err := os.Remove(filepath.Join(dir, "data", "minecraft", "maps", "0.dat")); err != nil {
		t.Fatalf("remove map 0: %v", err)
	}
	again, ok := maps.Get(0)
	if !ok || again != data {
		t.Errorf("Get after the file's removal = %p, %v; want the first read", again, ok)
	}
}

// TestMapsReadTheDefaults checks the codec's own defaults: a file with no
// scale and no locked flag is a plain map — none of either — and one whose
// colours are not the whole face stays blank. A map id no file answers, or a
// negative one — which names no file the game's own identifiers would read —
// is no map at all.
func TestMapsReadTheDefaults(t *testing.T) {
	dir := t.TempDir()
	writeMapData(t, dir, "3.dat", nbt.Compound{
		"dimension": nbt.String("minecraft:overworld"),
		"xCenter":   nbt.Int(0),
		"zCenter":   nbt.Int(0),
		"colors":    nbt.ByteArray(make([]byte, MapSide)),
		"scale":     nbt.Byte(15), // past the range, to the clamp's 4
		"locked":    nbt.Byte(1),
		"frames":    nbt.List{},
	})
	maps := NewMaps(dir)
	data, ok := maps.Get(3)
	if !ok {
		t.Fatal("map 3 did not read, want the file the test wrote")
	}
	if data.Scale != 4 {
		t.Errorf("the scale = %d, want the clamp of 15 at 4", data.Scale)
	}
	if data.Colors != [MapSide * MapSide]byte{} {
		t.Errorf("a wrong-length colours array = %v..., want the blank face", data.Colors[:8])
	}

	// No scale and no locked flag: the codec's defaults, 0 and false.
	writeMapData(t, dir, "4.dat", nbt.Compound{
		"dimension": nbt.String("minecraft:overworld"),
		"xCenter":   nbt.Int(0),
		"zCenter":   nbt.Int(0),
		"colors":    nbt.ByteArray(make([]byte, MapSide*MapSide)),
	})
	data, ok = maps.Get(4)
	if !ok {
		t.Fatal("map 4 did not read, want the file the test wrote")
	}
	if data.Scale != 0 || data.Locked {
		t.Errorf("the defaults = scale %d, locked %v; want 0 and false", data.Scale, data.Locked)
	}

	// No file, and a negative id: absent, not an error.
	if data, ok := maps.Get(5); ok {
		t.Errorf("map 5 = %+v, want no such map", data)
	}
	if data, ok := maps.Get(-1); ok {
		t.Errorf("map -1 = %+v, want no such map", data)
	}
}

// TestMapsRefuseAFileWithoutItsShape checks the codec's required fields: a
// file without its dimension or either centre is no map at all, the shape
// a missing one fails to decode to.
// vanilla: MapItemSavedData.CODEC, SavedDataStorage.readSavedData
func TestMapsRefuseAFileWithoutItsShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		drop string
	}{
		{"no dimension", "dimension"},
		{"no x centre", "xCenter"},
		{"no z centre", "zCenter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data := nbt.Compound{
				"dimension": nbt.String("minecraft:overworld"),
				"xCenter":   nbt.Int(64),
				"zCenter":   nbt.Int(64),
				"colors":    nbt.ByteArray(make([]byte, MapSide*MapSide)),
			}
			delete(data, tc.drop)
			writeMapData(t, dir, "0.dat", data)
			if got, ok := NewMaps(dir).Get(0); ok {
				t.Errorf("map 0 = %+v, want no map", got)
			}
		})
	}
}

// TestMapsPlaceTheSavedDecorations checks the markers a file's own banners
// and frames carry: each placed at the pixel its position makes, the banner
// keeping its colour and its name, the frame its rotation.
// vanilla: MapItemSavedData's constructor, addDecoration
func TestMapsPlaceTheSavedDecorations(t *testing.T) {
	dir := t.TempDir()
	writeMapData(t, dir, "0.dat", nbt.Compound{
		"dimension": nbt.String("minecraft:overworld"),
		"xCenter":   nbt.Int(64),
		"zCenter":   nbt.Int(64),
		"colors":    nbt.ByteArray(make([]byte, MapSide*MapSide)),
		"banners": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"pos":   nbt.IntArray{66, 70, 68},
				"color": nbt.String("red"),
				"name":  nbt.Compound{"text": nbt.String("Home")},
			},
		}},
		"frames": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"pos":       nbt.IntArray{64, 70, 60},
				"rotation":  nbt.Int(90),
				"entity_id": nbt.Int(3),
			},
		}},
	})
	data, ok := NewMaps(dir).Get(0)
	if !ok {
		t.Fatal("map 0 did not read, want the file the test wrote")
	}
	if len(data.Decorations) != 2 {
		t.Fatalf("the map carries %d markers, want the banner and the frame", len(data.Decorations))
	}
	banner := data.Decorations[0]
	if banner.Type != 24 || banner.X != 4 || banner.Y != 8 || banner.Rot != 8 {
		t.Errorf("the banner marker = type %d at (%d, %d) rot %d, want the red banner at (4, 8) rot 8",
			banner.Type, banner.X, banner.Y, banner.Rot)
	}
	if banner.Name == nil || banner.Name.Plain() != "Home" {
		t.Errorf("the banner marker's name = %v, want Home", banner.Name)
	}
	frame := data.Decorations[1]
	if frame.Type != 1 || frame.X != 0 || frame.Y != -7 || frame.Rot != 4 {
		t.Errorf("the frame marker = type %d at (%d, %d) rot %d, want the frame at (0, -7) rot 4",
			frame.Type, frame.X, frame.Y, frame.Rot)
	}
}

// TestPlaceDecoration checks the placement a marker gets: the clamp at the
// map's edge, the off-map types a player past it takes, the nether's own
// scramble of the level's time, and the edge case of a marker a map that
// does not track past its edge has none of.
// vanilla: MapItemSavedData.addDecoration, clampMapCoordinate, calculateRotation
func TestPlaceDecoration(t *testing.T) {
	overworld := &MapData{CenterX: 64, CenterZ: 64, Scale: 1, Dimension: "minecraft:overworld", TrackingPosition: true}

	// A player on the face: the player marker at the clamped pixel.
	got, ok := overworld.PlaceDecoration(DecorationPlayer, 0.5, 0.5, 0, 0, true)
	if !ok || got != (Decoration{Type: DecorationPlayer, X: -63, Y: -63, Rot: 0}) {
		t.Errorf("a player on the face = %+v, %v; want the player marker at (-63, -63)", got, ok)
	}

	// A player past the edge but within the off-map radius, and one past
	// it: the two off-map types, both pinned to the edge.
	got, ok = overworld.PlaceDecoration(DecorationPlayer, 264, 64, 0, 0, true)
	if !ok || got != (Decoration{Type: DecorationPlayerOffMap, X: 127, Y: 0, Rot: 0}) {
		t.Errorf("a player off the map = %+v, %v; want the off-map marker at (127, 0)", got, ok)
	}
	offLimits := &MapData{CenterX: 64, CenterZ: 64, Scale: 1, Dimension: "minecraft:overworld", UnlimitedTracking: true}
	got, ok = offLimits.PlaceDecoration(DecorationPlayer, 1000, 64, 0, 0, true)
	if !ok || got != (Decoration{Type: DecorationPlayerOffLimits, X: 127, Y: 0, Rot: 0}) {
		t.Errorf("a player past the limits = %+v, %v; want the off-limits marker at (127, 0)", got, ok)
	}
	if _, ok := overworld.PlaceDecoration(DecorationPlayer, 1000, 64, 0, 0, true); ok {
		t.Error("a player past the limits on a map that does not track them = a marker, want none")
	}

	// The nether's own scramble of the level's time, not the degrees: at
	// a game time of 100 the rotation is 10 where the overworld formula
	// would answer 0.
	nether := &MapData{CenterX: 64, CenterZ: 64, Dimension: NetherName, TrackingPosition: true}
	got, ok = nether.PlaceDecoration(DecorationPlayer, 64, 64, 0, 100, true)
	if !ok || got.Rot != 10 {
		t.Errorf("a nether player's marker = %+v, %v; want rot 10", got, ok)
	}
	// Off a level — the file's own markers — even a nether map turns by
	// the degrees.
	got, ok = nether.PlaceDecoration(DecorationFrame, 64, 64, 0, 100, false)
	if !ok || got.Rot != 0 {
		t.Errorf("a nether map's file marker = %+v, %v; want rot 0", got, ok)
	}
	// The end is no nether: its markers turn by the degrees, 90 reading
	// as the eighth of a circle like everywhere but the nether.
	end := &MapData{CenterX: 64, CenterZ: 64, Dimension: EndName, TrackingPosition: true}
	got, ok = end.PlaceDecoration(DecorationPlayer, 64, 64, 90, 100, true)
	if !ok || got.Rot != 4 {
		t.Errorf("an end player's marker = %+v, %v; want rot 4", got, ok)
	}

	// A banner off a map that does not track past its edge has nowhere to
	// sit; one an unlimited map tracks still pins to the edge.
	if _, ok := overworld.PlaceDecoration(DecorationFrame, 264, 64, 0, 0, true); ok {
		t.Error("a banner off the map = a marker, want none")
	}
	got, ok = offLimits.PlaceDecoration(DecorationFrame, 264, 64, 0, 0, true)
	if !ok || got != (Decoration{Type: DecorationFrame, X: 127, Y: 0, Rot: 0}) {
		t.Errorf("a banner off an unlimited map = %+v, %v; want the frame marker at (127, 0)", got, ok)
	}
}

// TestMapDrawsAWaterloggedBlockAsItsWater checks the colour a fluid block
// contributes: a waterlogged slab's top face is not full support, so the map
// draws the water logging it rather than the slab's own colour.
// vanilla: MapItem.getCorrectStateForFluidBlock
func TestMapDrawsAWaterloggedBlockAsItsWater(t *testing.T) {
	dir := t.TempDir()
	w := newTestWorld(t, NewFlatGenerator())
	// The face centres on (0, 0), so its middle pixel (64, 64) is the cell
	// the slab goes in; only a loaded chunk is drawn, so the origin chunk
	// must be up.
	w.Acquire(ChunkPos{X: 0, Z: 0})
	slab, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	if !ok {
		t.Fatal("no waterlogged oak slab state")
	}
	if !w.SetBlock(0, GroundLevel, 0, slab) {
		t.Fatal("the waterlogged slab did not go into the world")
	}
	writeMapData(t, dir, "0.dat", nbt.Compound{
		"dimension": nbt.String(DimensionName),
		"xCenter":   nbt.Int(0),
		"zCenter":   nbt.Int(0),
		"colors":    nbt.ByteArray(make([]byte, MapSide*MapSide)),
	})
	maps := NewMaps(dir)
	data, ok := maps.Get(0)
	if !ok {
		t.Fatal("map 0 did not read")
	}
	maps.Draw(0, w, 0.5, 0.5)
	if got := data.Colors[64+64*MapSide] >> 2; got != mapColorWater {
		t.Errorf("the waterlogged slab's pixel = colour %d, want water %d", got, mapColorWater)
	}
}

// TestMapUpdateRedrawsOneStripe checks the held map's per-tick sweep: the one
// column in sixteen the step selects is resampled and the rectangle the
// changed pixels span is reported, and a second pass over an unchanged column
// reports nothing to send. The rectangle spans the sixteen loaded columns
// because a column after one that changed is sampled too, so the seam of a
// redraw that moved the surface leaves no stale strip behind.
// vanilla: MapItem.update, MapItemSavedData.HoldingPlayer
func TestMapUpdateRedrawsOneStripe(t *testing.T) {
	dir := t.TempDir()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{X: 0, Z: 0})
	writeMapData(t, dir, "0.dat", nbt.Compound{
		"dimension": nbt.String(DimensionName),
		"xCenter":   nbt.Int(0),
		"zCenter":   nbt.Int(0),
		"colors":    nbt.ByteArray(make([]byte, MapSide*MapSide)),
	})
	maps := NewMaps(dir)
	if _, ok := maps.Get(0); !ok {
		t.Fatal("map 0 did not read")
	}
	patch, changed := maps.Update(0, w, 0.5, 0.5, 0)
	if !changed {
		t.Fatal("the first stripe of a blank map over the loaded chunk changed nothing")
	}
	if want := (MapPatch{X: 64, Z: 64, Width: 16, Height: 16}); patch != want {
		t.Errorf("the first stripe's rectangle = %+v, want the loaded chunk's corner %+v", patch, want)
	}
	if _, changed := maps.Update(0, w, 0.5, 0.5, 0); changed {
		t.Error("a second pass over the same stripe reported a change, want none")
	}
}

func TestMapsScaleOnlyBelowFourAndKeepTheCentre(t *testing.T) {
	dir := t.TempDir()
	maps := NewMaps(dir)
	id, data := maps.Create("minecraft:overworld", 10, 10, 3)
	if !maps.Scalable(id) {
		t.Fatal("a scale 3 map is not scalable, want one")
	}
	bigger, ok := maps.Scaled(id)
	if !ok || bigger == id {
		t.Fatalf("Scaled = %d, %t, want a fresh id", bigger, ok)
	}
	got, _ := maps.Get(bigger)
	if got.Scale != 4 || got.Dimension != data.Dimension {
		t.Errorf("scaled map = scale %d in %s, want scale 4 in %s", got.Scale, got.Dimension, data.Dimension)
	}
	if maps.Scalable(bigger) {
		t.Error("a scale 4 map is scalable, want it at the limit")
	}
	if maps.Scalable(99) {
		t.Error("a map without saved data is scalable")
	}
}
