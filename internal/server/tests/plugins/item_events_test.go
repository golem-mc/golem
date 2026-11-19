package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
)

// vanilla: ItemEntity.tick
func TestPluginItemDespawnEvent(t *testing.T) {
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
