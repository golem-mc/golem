package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: PrimedTnt.explode, ServerExplosion.explode
func TestPluginEntityExplosionEvents(t *testing.T) {
	var primed, exploded int
	var cancelPrime, cancelExplode bool
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ExplosionPrimeEvent) {
			primed++
			e.SetCancelled(cancelPrime)
		})
		plugin.Listen(s, func(e *plugin.EntityExplodeEvent) {
			exploded++
			e.SetCancelled(cancelExplode)
		})
	})
	cancelPrime = true
	srv.SpawnTNT(4.5, 62, 4.5, 1)
	for range 3 {
		srv.RunTick()
	}
	if primed != 1 || exploded != 0 {
		t.Fatalf("cancelled prime: primed %d, exploded %d", primed, exploded)
	}
	cancelPrime, cancelExplode = false, true
	srv.SpawnTNT(4.5, 62, 4.5, 1)
	for range 3 {
		srv.RunTick()
	}
	if primed != 2 || exploded != 1 {
		t.Errorf("cancelled explode: primed %d, exploded %d", primed, exploded)
	}
}

// vanilla: ItemEntity.tick
func TestPluginEntityRemoveEvent(t *testing.T) {
	var got []plugin.RemoveCause
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityRemoveEvent) { got = append(got, e.Cause) })
	})
	id := srv.SpawnItemEntity(0.5, 66, 10.5, "minecraft:dirt", 3)
	srv.SetItemAge(id, 5999)
	for range 4 {
		srv.RunTick()
	}
	if len(got) != 1 || got[0] != plugin.RemoveDespawn {
		t.Errorf("removals %v, want one despawn", got)
	}
}

// vanilla: Animal.spawnChildFromBreeding
func TestPluginEntityBreedEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityBreedEvent) {
			asked++
			_ = e.Mother.Health() // reads the parents: no lock may be held
			_ = e.Father.Health()
			e.SetCancelled(cancel)
		})
	})
	a := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	b := srv.SpawnMob("minecraft:cow", 5.5, 64, 4.5)
	srv.SetMobInLove(a, 600)
	srv.SetMobInLove(b, 600)
	ticksUntil(t, srv, "the breeding is asked", func() bool { srv.TickMobs(1); return asked > 0 })
	if n := srv.MobCount(); n != 2 {
		t.Fatalf("cancelled breeding left %d mobs, want 2", n)
	}
	cancel = false
	srv.SetMobInLove(a, 600)
	srv.SetMobInLove(b, 600)
	ticksUntil(t, srv, "the calf is born", func() bool { srv.TickMobs(1); return srv.MobCount() == 3 })
}

// vanilla: PiglinAi.stopHoldingOffHandItem
func TestPluginPiglinBarterEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PiglinBarterEvent) {
			asked++
			_ = e.Piglin.Health() // reads the piglin: no lock may be held
			if e.Input.Item != registry.ItemGoldIngot {
				t.Errorf("input %v", e.Input)
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:piglin", 4.5, 64, 4.5)
	for _, want := range []int{0, 1} {
		if !srv.OfferGold(id) {
			t.Fatal("the piglin refused the ingot")
		}
		before := srv.ItemCount()
		ticksUntil(t, srv, "the barter is asked", func() bool { srv.TickMobs(1); return asked > want })
		srv.TickMobs(2)
		if got := srv.ItemCount() - before; (want == 0) != (got == 0) {
			t.Fatalf("cancel=%v: %d items thrown", cancel, got)
		}
		cancel = false
	}
}

// vanilla: HangingEntityItem.useOn
func TestPluginHangingPlaceEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _, c := blockScene(t, "minecraft:item_frame", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.HangingPlaceEvent) {
			asked++
			_ = e.Entity.Facing() // reads the frame: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	c.MoveToGround(4.5, 64, 3.5, true)
	c.Settle()
	c.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	fired(t, srv, c, "hanging place", &asked, 1)
	if n := srv.FrameCount(); n != 0 {
		t.Fatalf("cancelled place left %d frames", n)
	}
	cancel = false
	c.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	fired(t, srv, c, "second hanging place", &asked, 2)
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("allowed place left %d frames, want 1", n)
	}
}

// vanilla: Villager.updateTrades
func TestPluginVillagerAcquireTradeEvent(t *testing.T) {
	asked := 0
	srv, _, c := blockScene(t, "minecraft:dirt", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.VillagerAcquireTradeEvent) {
			asked++
			_ = e.Villager.Level() // reads the villager: no lock may be held
			if asked == 1 {
				e.SetCancelled(true)
			} else {
				e.Trade.MaxUses = 99
			}
		})
	})
	id := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, nil, 1.5, 64, 0.5)
	c.Settle()
	c.UseOn(id)
	fired(t, srv, c, "acquired trade", &asked, 2)
	offers, _ := srv.VillagerSaved(id)
	recipes, _ := nbt.Get[nbt.List](offers, "Recipes")
	if len(recipes.Items) == 0 {
		t.Fatal("every trade was dropped")
	}
	for _, item := range recipes.Items {
		r, _ := nbt.CompoundView(item)
		if n, _ := nbt.IntValue(r["maxUses"]); n != 99 {
			t.Errorf("maxUses = %d, want the edited 99", n)
		}
	}
}
