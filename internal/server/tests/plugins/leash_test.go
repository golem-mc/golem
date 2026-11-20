package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.interact, Leashable.dropLeash
func TestPluginPlayerLeashEntityEvent(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	var last *plugin.PlayerLeashEntityEvent
	srv, _, c := blockScene(t, "minecraft:lead", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerLeashEntityEvent) {
			asked++
			last = e
			if e.Entity.Type() != registry.EntityTypeCow || e.Holder.ID() != e.Player.ID() {
				t.Errorf("entity %v holder %v", e.Entity.Type(), e.Holder)
			}
			e.SetCancelled(cancel)
		})
	})
	cow := srv.SpawnMob("minecraft:cow", 1.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow)
	fired(t, srv, c, "leash event", &asked, 1)
	if holder, ok := srv.MobLeashHolder(cow); ok && holder != 0 {
		t.Fatalf("a cancelled leash tied the cow to %d", holder)
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot); got.Count != 1 {
		t.Fatalf("the cancelled leash spent the lead: %d left", got.Count)
	}
	cancel = false
	c.UseOn(cow)
	fired(t, srv, c, "leash event", &asked, 2)
	if holder, ok := srv.MobLeashHolder(cow); !ok || holder != c.PlayerEntityID || last.Unleash {
		t.Fatalf("an allowed leash: holder %d, unleash %v", holder, last.Unleash)
	}
	c.UseOn(cow)
	fired(t, srv, c, "unleash event", &asked, 3)
	if !last.Unleash {
		t.Error("taking the lead off was not an Unleash")
	}
}
