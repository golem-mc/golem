package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestMapLootCreatesASavedDestinationMap(t *testing.T) {
	root := t.TempDir()
	w, err := world.NewWorld(root, world.NewOverworldGenerator(0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	s := withDims(&Server{maps: world.NewMaps(root)}, []*dimension{{w: w, name: "minecraft:overworld"}})
	ctx := s.mapLootContext(w, 272, 64, 944)
	stack := protocol.ItemStack{ItemID: int32(registry.ItemPlainsVillageMap), Count: 1}
	if !ctx.ExplorationMap(registry.LootExplorationMap{Destination: []string{"minecraft:village_plains"}, Decoration: "minecraft:village_plains", Zoom: 2, SearchRadius: 0}, &stack) {
		t.Fatal("known village not found")
	}
	if registry.Item(stack.ItemID) != registry.ItemPlainsVillageMap {
		t.Fatalf("the map became item %d", stack.ItemID)
	}
	id, ok := stack.MapIDOf()
	if !ok {
		t.Fatal("map ID missing")
	}
	data, ok := s.maps.Get(id)
	if !ok || data.Scale != 2 || !data.TrackingPosition || !data.UnlimitedTracking || data.Dimension != "minecraft:overworld" {
		t.Fatalf("map data %+v", data)
	}
	target := stack.MapDecorations["+"]
	if target.Type != "minecraft:village_plains" || target.X != 272 || target.Z != 944 || target.Rotation != 180 {
		t.Fatalf("target %+v", target)
	}
	if w.LoadedChunks() != 0 {
		t.Fatal("preview generated terrain chunks")
	}
	s.maps.Save()
	saved, ok := world.NewMaps(root).Get(id)
	if !ok || saved.Colors != data.Colors || !saved.UnlimitedTracking {
		t.Fatal("map data did not persist")
	}
}
