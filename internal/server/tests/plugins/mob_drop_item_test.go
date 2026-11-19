package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Chicken.aiStep, Entity.spawnAtLocation
func TestPluginEntityDropItemEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDropItemEvent) {
			asked++
			if e.Entity.Type() != registry.EntityTypeChicken {
				t.Errorf("dropper is %v", e.Entity.Type())
			}
			_ = e.Item.Age() // reads both entities: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	srv.SetEggTimer(id, 1)
	srv.TickMobs(1)
	if asked != 1 || srv.ItemCount() != 0 {
		t.Fatalf("cancelled drop: asked %d, items %d", asked, srv.ItemCount())
	}
	cancel = false
	srv.SetEggTimer(id, 1)
	srv.TickMobs(1)
	if asked != 2 || srv.ItemCount() != 1 {
		t.Errorf("allowed drop: asked %d, items %d", asked, srv.ItemCount())
	}
}
