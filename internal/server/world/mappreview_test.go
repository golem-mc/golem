package world

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"sync"
	"testing"
)

func TestExplorerPreviewPaintsWaterCoastsAndLeavesBorders(t *testing.T) {
	data := MapData{CenterX: 64, CenterZ: 64}
	for i := range data.Colors {
		data.Colors[i] = 77
	}
	calls := 0
	renderBiomePreview(&data, func(x, y, z int32) string {
		calls++
		if y != 0 {
			t.Fatal("preview did not sample sea-level biome")
		}
		if x == 64 && z == 64 {
			return "minecraft:ocean"
		}
		return "minecraft:plains"
	})
	for _, tc := range []struct {
		x, z int
		want byte
	}{{64, 64, 63}, {63, 64, 107}, {65, 65, 107}, {62, 64, 77}, {0, 0, 77}, {127, 127, 77}} {
		if got := data.Colors[tc.z*MapSide+tc.x]; got != tc.want {
			t.Fatalf("pixel %d,%d=%d, want %d", tc.x, tc.z, got, tc.want)
		}
	}
	if calls != MapSide*MapSide {
		t.Fatalf("sampled %d biomes", calls)
	}
	renderBiomePreview(&data, func(_, _, _ int32) string { return "minecraft:ocean" })
	// Mth.sin(2)*7 truncates to 6, so x=10,18,26,34 cover wave cases 2,3,4,0.
	for _, tc := range []struct {
		x    int
		want byte
	}{{10, 62}, {18, 61}, {26, 60}, {34, 60}} {
		if got := data.Colors[2*MapSide+tc.x]; got != tc.want {
			t.Fatalf("deep-water pixel %d=%d, want %d", tc.x, got, tc.want)
		}
	}
	if data.Colors[3*MapSide+10] != 77 {
		t.Fatal("deep-water odd row was painted")
	}
}

func TestExplorerPreviewScalesTruncatesCentersAndSaves(t *testing.T) {
	dir := t.TempDir()
	m := NewMaps(dir)
	id, data := m.Create(DimensionName, 0, 0, 2)
	data.CenterX, data.CenterZ = -3, -5
	calls := 0
	m.RenderBiomePreview(id, NetherName, func(_, _, _ int32) string { t.Fatal("other dimension was sampled"); return "" })
	m.RenderBiomePreview(id, DimensionName, func(x, y, z int32) string {
		if calls == 0 && (x != -256 || z != -260) {
			t.Fatalf("first sample (%d,%d), want (-256,-260)", x, z)
		}
		calls++
		return "minecraft:ocean"
	})
	m.Save()
	restored, ok := NewMaps(dir).Get(id)
	if !ok || restored.Colors != data.Colors || restored.Colors[2*MapSide+10] == 0 || calls != MapSide*MapSide {
		t.Fatal("saved map lost biome preview")
	}
}

func TestMapBiomeReaderUsesZoomAndUnloadedBiomes(t *testing.T) {
	g := NewOverworldGenerator(0)
	w := newTestWorld(t, g)
	read := w.MapBiomeReader()
	probe := g.NewBiomeProbe()
	for _, p := range [][3]int32{{272, 0, 944}, {-34, 0, -125}, {0, 0, 0}} {
		qx, qy, qz := levelgen.ZoomedQuart(g.zoomSeed, p[0], p[1], p[2])
		if got, want := read(p[0], p[1], p[2]), probe(qx, qy, qz); got != want {
			t.Fatalf("biome %q, want %q", got, want)
		}
	}
	if w.LoadedChunks() != 0 {
		t.Fatal("biome lookup generated terrain")
	}
}

func TestCarriedMapTargetsAreImportedOnceAndReloadedFromTheItem(t *testing.T) {
	root := t.TempDir()
	m := NewMaps(root)
	id, data := m.Create("minecraft:overworld", 272, 944, 2)
	entries := map[string]protocol.MapDecoration{"+": {Type: "minecraft:village_plains", X: 272, Z: 944, Rotation: 180}}
	var holders sync.WaitGroup
	for range 8 {
		holders.Go(func() {
			for range 2 {
				m.AddItemDecorations(id, entries)
			}
		})
	}
	holders.Wait()
	if len(data.ItemDecorations) != 1 || data.ItemDecorations[0].Type != 28 {
		t.Fatalf("targets %+v", data.ItemDecorations)
	}
	m.Save()
	reloaded := NewMaps(root)
	reloaded.AddItemDecorations(id, entries)
	saved, _ := reloaded.Get(id)
	if len(saved.ItemDecorations) != 1 {
		t.Fatal("reloaded item did not restore its target")
	}
}
