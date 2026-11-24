package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemEntity.tick
func TestPluginItemDespawnEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ItemDespawnEvent) {
			asked++
			_ = e.Item.Age() // reads the entity: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnItemEntity(0.5, 66, 10.5, "minecraft:dirt", 3)
	srv.SetItemAge(id, 5999)
	for range 4 {
		srv.RunTick()
	}
	if asked != 1 || srv.ItemCount() != 1 {
		t.Fatalf("cancelled despawn: asked %d, items %d", asked, srv.ItemCount())
	}
	cancel = false
	srv.SetItemAge(id, 5999)
	for range 4 {
		srv.RunTick()
	}
	if asked != 2 || srv.ItemCount() != 0 {
		t.Errorf("allowed despawn: asked %d, items %d", asked, srv.ItemCount())
	}
}

// vanilla: ItemEntity.tryToMerge
func TestPluginItemMergeEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ItemMergeEvent) {
			asked++
			_ = e.Target.Age()
			e.SetCancelled(cancel)
		})
	})
	srv.SpawnItemEntity(0.5, 66, 10.5, "minecraft:dirt", 3)
	srv.SpawnItemEntity(0.5, 66, 10.5, "minecraft:dirt", 5)
	// A settled item scans for neighbours every 40th tick.
	for range 90 {
		srv.RunTick()
	}
	if asked == 0 || srv.ItemCount() != 2 {
		t.Fatalf("cancelled merge: asked %d, items %d", asked, srv.ItemCount())
	}
	cancel = false
	for range 90 {
		srv.RunTick()
	}
	if srv.ItemCount() != 1 {
		t.Errorf("allowed merge: items %d", srv.ItemCount())
	}
}

// vanilla: ItemEntity.mergeWithNeighbours
func TestItemMergeOrderAcrossWorldsAfterGroupShrinks(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	srv, _ := tickScene(t, func(s plugin.Server) { host = s })
	other, err := host.CreateWorld(flatOptions(&flatGen{}))
	if err != nil {
		t.Fatal(err)
	}
	over := host.Overworld()
	defer srv.HoldChunkInDimension(other.Key().String(), 0, 10)()
	defer srv.HoldChunkInDimension(over.Key().String(), 0, 10)()
	drop := func(w plugin.World, x float64, count int) plugin.ItemEntity {
		t.Helper()
		it := w.DropItem(plugin.Vec3{X: x, Y: 70, Z: 10.5}, plugin.NewStack(registry.ItemDirt, count), false)
		if it == nil {
			t.Fatal("drop dirt failed")
		}
		it.SetNoGravity(true)
		return it
	}
	for range 32 {
		drop(over, 8.5, 1)
	}
	for range 40 {
		srv.RunTick()
	}
	if got := srv.ItemCount(); got != 1 {
		t.Fatalf("large group left %d stacks, want 1", got)
	}

	foreign := drop(other, 1.9, 33)
	scanner := drop(over, 1.9, 21)
	// The first neighbour is in the next grid cell. The second is in the
	// scanner's cell but was registered later. Only one can fit.
	first := drop(over, 2.2, 33)
	second := drop(over, 1.7, 33)
	for range 40 {
		srv.RunTick()
	}
	if scanner.IsValid() {
		t.Error("the scanning stack survived the merge")
	}
	if got := first.Stack().Count; got != 54 {
		t.Errorf("first neighbour has %d dirt, want 54", got)
	}
	if got := second.Stack().Count; got != 33 {
		t.Errorf("second neighbour has %d dirt, want 33", got)
	}
	if got := foreign.Stack().Count; got != 33 {
		t.Errorf("other world's stack has %d dirt, want 33", got)
	}
	if got := srv.ItemCount(); got != 4 {
		t.Errorf("both worlds hold %d stacks, want 4", got)
	}
}
