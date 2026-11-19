package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginVillagerAndVehicleHandles(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		v, ok := spawnAt(t, w, registry.EntityTypeVillager, nil).(plugin.Villager)
		if !ok {
			t.Fatal("villager is not a Villager")
		}
		v.SetProfession(plugin.MinecraftKey("librarian"))
		v.SetLevel(3)
		v.SetExperience(75)
		if v.Profession().String() != "minecraft:librarian" || v.Level() != 3 || v.Experience() != 75 {
			t.Errorf("villager data: %v %d %d", v.Profession(), v.Level(), v.Experience())
		}
		v.SetTrades([]plugin.Trade{{
			Cost:    plugin.ItemStack{Item: registry.ItemEmerald, Count: 5},
			Cost2:   plugin.ItemStack{Item: registry.ItemBook, Count: 1},
			Result:  plugin.ItemStack{Item: registry.ItemDiamond, Count: 2},
			Uses:    1,
			MaxUses: 9, XP: 7, PriceMultiplier: 0.5, Demand: 2, SpecialPrice: -1, RewardsExperience: true,
		}})
		got := v.Trades()
		if len(got) != 1 || got[0].Cost.Count != 5 || got[0].Cost2.Item != registry.ItemBook ||
			got[0].Result.Item != registry.ItemDiamond || got[0].MaxUses != 9 || got[0].XP != 7 ||
			got[0].PriceMultiplier != 0.5 || got[0].SpecialPrice != -1 || !got[0].RewardsExperience {
			t.Errorf("trades: %+v", got)
		}
		if _, trading := v.Trader(); trading {
			t.Error("idle villager has a trader")
		}
		if _, ok := spawnAt(t, w, registry.EntityTypeCow, nil).(plugin.Villager); ok {
			t.Error("cow is a Villager")
		}

		cart, ok := spawnAt(t, w, registry.EntityTypeMinecart, nil).(plugin.Vehicle)
		if !ok {
			t.Fatal("minecart is not a Vehicle")
		}
		cart.SetDamageTaken(12)
		if cart.DamageTaken() != 12 {
			t.Errorf("cart damage %v", cart.DamageTaken())
		}
		boat, ok := spawnAt(t, w, registry.EntityTypeOakBoat, nil).(plugin.Vehicle)
		if !ok {
			t.Fatal("boat is not a Vehicle")
		}
		boat.SetDamageTaken(7)
		if boat.DamageTaken() != 7 {
			t.Errorf("boat damage %v", boat.DamageTaken())
		}
		if _, ok := spawnAt(t, w, registry.EntityTypeCow, nil).(plugin.Vehicle); ok {
			t.Error("cow is a Vehicle")
		}
	})
}

func TestPluginMobMovementAndTarget(t *testing.T) {
	srv, w, run, cmd, _ := kindsFixture(t)
	srv.OverworldWorld().SetChunkForced(world.ChunkPos{X: 0, Z: 0}, true) // entity-ticks with no player
	cmd("fill 0 79 0 15 79 15 minecraft:stone")
	servertest.HoldTicks(t, srv)
	var cow, calf plugin.Mob
	run(func() {
		cow = spawnAt(t, w, registry.EntityTypeCow, nil).(plugin.Mob)
		if !cow.(plugin.Breedable).IsBreedingItem(plugin.ItemStack{Item: registry.ItemWheat, Count: 1}) ||
			cow.(plugin.Breedable).IsBreedingItem(plugin.ItemStack{Item: registry.ItemStick, Count: 1}) {
			t.Error("breeding items")
		}
		cow.SetArrowsInBody(3)
		cow.SetNoDamageTicks(7)
		cow.SetCollidable(false)
		if cow.ArrowsInBody() != 3 || cow.NoDamageTicks() != 7 || cow.Collidable() {
			t.Errorf("living state %d %d %v", cow.ArrowsInBody(), cow.NoDamageTicks(), cow.Collidable())
		}
		cow.SetCollidable(true)
		if _, _, ok := cow.ActiveItem(); ok {
			t.Error("a cow uses an item")
		}
		if _, ok := cow.Target(); ok {
			t.Error("a fresh cow has a target")
		}
		if !cow.MoveTo(plugin.Vec3{X: 12.5, Y: 80, Z: 8.5}, 1) {
			t.Fatal("no path on open ground")
		}
		calf = spawnAt(t, w, registry.EntityTypeSheep, nil).(plugin.Mob)
		calf.SetLeash(cow)
		if h, ok := calf.Leash(); !ok || h.UUID() != cow.UUID() {
			t.Errorf("leash %v %v", h, ok)
		}
		calf.SetLeash(nil)
		if _, ok := calf.Leash(); ok {
			t.Error("leash survived being cut")
		}
	})
	servertest.RunTicks(t, srv, 60)
	if x := cow.Pos().X; x < 11 {
		t.Errorf("the cow stopped at x=%v", x)
	}
	run(func() {
		cow.MoveTo(plugin.Vec3{X: 2.5, Y: 80, Z: 8.5}, 1)
		cow.StopMoving()
		cow.SetAI(false)
		if cow.MoveTo(plugin.Vec3{X: 2.5, Y: 80, Z: 8.5}, 1) {
			t.Error("a mob without AI walked")
		}
	})
}
