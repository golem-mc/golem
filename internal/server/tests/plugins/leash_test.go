package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// vanilla: AbstractBoat.interact, Entity.interact
func TestPluginPlayerLeashBoat(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	var last *plugin.PlayerLeashEntityEvent
	srv, _, c := blockScene(t, "minecraft:lead", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerLeashEntityEvent) {
			asked++
			last = e
			if e.Entity.Type() != registry.EntityTypeOakBoat || e.Holder.ID() != e.Player.ID() {
				t.Errorf("entity %v holder %v", e.Entity.Type(), e.Holder)
			}
			e.SetCancelled(cancel)
		})
	})
	boat := srv.SpawnBoat(1.5, 64, 1.5, "minecraft:oak_boat")
	c.Settle()
	c.UseOn(boat)
	fired(t, srv, c, "boat leash cancel", &asked, 1)
	if holder, ok := srv.BoatLeashHolder(boat); ok && holder != 0 {
		t.Fatalf("cancelled boat leash tied boat to %d", holder)
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot); got.Count != 1 {
		t.Fatalf("cancelled boat leash spent the lead: %d left", got.Count)
	}
	cancel = false
	c.UseOn(boat)
	fired(t, srv, c, "boat leash allow", &asked, 2)
	if holder, ok := srv.BoatLeashHolder(boat); !ok || holder != c.PlayerEntityID || last.Unleash {
		t.Fatalf("allowed boat leash: holder %d, unleash %v", holder, last.Unleash)
	}
	c.UseOn(boat)
	fired(t, srv, c, "boat unleash", &asked, 3)
	if !last.Unleash {
		t.Error("taking the boat lead off was not an Unleash")
	}
}

// vanilla: LeashFenceKnotEntity.interact, Entity.interact
func TestPluginPlayerLeashKnotAndMobTransfer(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, false
	var last *plugin.PlayerLeashEntityEvent
	srv, _, c := blockScene(t, "minecraft:lead", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerLeashEntityEvent) {
			asked++
			last = e
			e.SetCancelled(cancel)
		})
	})
	srv.SetBlockAndTell(2, 64, 0, servertest.MustState(t, "minecraft:oak_fence", nil))
	cow1 := srv.SpawnMob("minecraft:cow", 1.5, 64, 1.5)
	cow2 := srv.SpawnMob("minecraft:cow", 1.5, 64, -1.5)
	cow3 := srv.SpawnMob("minecraft:cow", -1.5, 64, 0.5)
	srv.SetMobNoAI(cow1)
	srv.SetMobNoAI(cow2)
	srv.SetMobNoAI(cow3)
	srv.SetMobLeashHolder(cow1, c.PlayerEntityID)
	c.Settle()

	// Tie cow1 onto the fence to create a knot.
	c.UseBlock(2, 64, 0)
	fired(t, srv, c, "tie cow1 to fence", &asked, 1)
	knotID, ok := srv.MobLeashHolder(cow1)
	if !ok || knotID == 0 || knotID == c.PlayerEntityID {
		t.Fatalf("cow1 not on knot: holder %d", knotID)
	}

	// Lead cow2 and click the knot: cancel keeps cow2 on player, allow moves it to knot.
	srv.SetMobLeashHolder(cow2, c.PlayerEntityID)
	cancel = true
	c.UseOn(knotID)
	fired(t, srv, c, "knot tie cancel", &asked, 2)
	if h, _ := srv.MobLeashHolder(cow2); h != c.PlayerEntityID {
		t.Fatalf("cancelled knot tie moved cow2 to %d", h)
	}
	cancel = false
	c.UseOn(knotID)
	fired(t, srv, c, "knot tie allow", &asked, 3)
	if h, _ := srv.MobLeashHolder(cow2); h != knotID || last.Holder.ID() != knotID {
		t.Fatalf("allowed knot tie: cow2 holder %d, event holder %d, want %d", h, last.Holder.ID(), knotID)
	}

	// Click the knot with no led mobs (take-back): cancel leaves both on knot, allow moves them to player.
	cancel = true
	c.UseOn(knotID)
	fired(t, srv, c, "knot takeback cancel", &asked, 5)
	if h, _ := srv.MobLeashHolder(cow1); h != knotID {
		t.Fatalf("cancelled take-back moved cow1 to %d", h)
	}
	cancel = false
	c.UseOn(knotID)
	fired(t, srv, c, "knot takeback allow", &asked, 7)
	if h, _ := srv.MobLeashHolder(cow1); h != c.PlayerEntityID {
		t.Fatalf("allowed take-back: cow1 holder %d, want %d", h, c.PlayerEntityID)
	}

	// Sneak-click cow3 to transfer led mobs onto cow3: cancel keeps cow1 on player, allow ties to cow3.
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	cancel = true
	c.UseOnSneaking(cow3)
	fired(t, srv, c, "mob transfer cancel", &asked, 9)
	if h, _ := srv.MobLeashHolder(cow1); h != c.PlayerEntityID {
		t.Fatalf("cancelled mob transfer moved cow1 to %d", h)
	}
	cancel = false
	c.UseOnSneaking(cow3)
	fired(t, srv, c, "mob transfer allow", &asked, 11)
	if h, _ := srv.MobLeashHolder(cow1); h != cow3 || last.Holder.ID() != cow3 {
		t.Fatalf("allowed mob transfer: cow1 holder %d, event holder %d, want %d", h, last.Holder.ID(), cow3)
	}
}
