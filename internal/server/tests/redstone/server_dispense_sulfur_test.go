package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SulfurCubeBlockDispenseItemBehavior.dispenseBlock
func TestDispenserSulfurAbsorptionSelectsEligibleCubeAndReplacesBlock(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	outside := srv.SpawnMob("minecraft:sulfur_cube", 6, 64, 1.5)
	baby := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 1.5)
	srv.SetMobBaby(baby, true)
	adult := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 1.5)
	for _, id := range []int32{outside, baby, adult} {
		srv.SetMobNoAI(id)
	}
	dispense := func(item string) {
		t.Helper()
		if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, item, 2)) {
			t.Fatal("missing dispenser")
		}
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != 1 {
			t.Fatalf("dispense spent %v, want one remaining", got)
		}
	}
	dispense("minecraft:stone")
	stone := registry.MustItem("minecraft:stone")
	if got := srv.MobEquipment(adult, "body"); got.ItemID != stone || got.Count != 1 {
		t.Fatalf("adult body = %+v", got)
	}
	for _, id := range []int32{outside, baby} {
		if !srv.MobEquipment(id, "body").Empty() {
			t.Fatalf("ineligible cube %d swallowed stone", id)
		}
	}
	// Same-item refusal falls back to dropping; the next distinct item ejects
	// the old body without consuming a second item from the dispenser.
	dispense("minecraft:stone")
	if got := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:stone"); got != 1 {
		t.Fatalf("same-item fallback dropped %d stone", got)
	}
	dispense("minecraft:oak_planks")
	if got := srv.MobEquipment(adult, "body"); got.ItemID != registry.MustItem("minecraft:oak_planks") {
		t.Fatalf("replacement body = %+v", got)
	}
	if got := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:stone"); got != 2 {
		t.Fatalf("replacement left %d stone, want refused plus ejected", got)
	}
	// TNT's explicit behaviour must try absorption instead of spawning primed TNT.
	dispense("minecraft:tnt")
	if got := srv.MobEquipment(adult, "body"); got.ItemID != registry.MustItem("minecraft:tnt") {
		t.Fatalf("TNT body = %+v", got)
	}
	srv.SetGameRule(world.RuleTNTExplodes.String(), false)
	dispense("minecraft:oak_planks")
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:tnt", 2))
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != 2 {
		t.Fatalf("disabled TNT spent: %+v", got)
	}
	if got := srv.MobEquipment(adult, "body"); got.ItemID != registry.MustItem("minecraft:oak_planks") {
		t.Fatalf("disabled TNT replaced body: %+v", got)
	}
}

func TestDispenserSulfurAbsorptionSkipsSameItemCube(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	first := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 1.5)
	srv.SetMobNoAI(first)
	for range 2 {
		srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:stone", 1))
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		if srv.MobEquipment(first, "body").Empty() {
			t.Fatal("first cube did not absorb")
		}
		if len(srv.Mobs()) == 1 {
			second := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 1.5)
			srv.SetMobNoAI(second)
		}
	}
	for _, m := range srv.Mobs() {
		if srv.MobEquipment(m.ID, "body").Empty() {
			t.Fatal("refusing cube stopped target search")
		}
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Fatalf("successful absorption dropped %+v", items)
	}
}

// vanilla: FlintAndSteelDispenseItemBehavior.tryIgniteExplosiveEntities
func TestADispenserLightsAnExplosiveSulfurCubesFuseWithItsFlintAndSteel(t *testing.T) {
	t.Parallel()
	for _, explosive := range []bool{true, false} {
		srv, w := servertest.Dispensing(t)
		cube := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 1.5)
		srv.SetMobNoAI(cube)
		if explosive {
			srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:tnt", 1))
			srv.Dispense(4, 64, 0, protocol.FaceSouth)
			if got := srv.MobEquipment(cube, "body"); got.ItemID != registry.MustItem("minecraft:tnt") {
				t.Fatalf("body = %+v, want TNT", got)
			}
		}
		srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1))
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		w.DrainRecorded()
		name, _ := servertest.StatePropsOf(t, srv, 4, 64, 1)
		wantBlock := "minecraft:fire"
		if explosive {
			wantBlock = "minecraft:air"
		}
		if name != wantBlock {
			t.Errorf("explosive=%v: target is %s, want %s", explosive, name, wantBlock)
		}
		if got := srv.MobSulfurPrimed(cube); got != explosive {
			t.Errorf("explosive=%v: primed = %v", explosive, got)
		}
		if !slices.Contains(w.Events, protocol.WorldEventDispense) || slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
			t.Errorf("explosive=%v: events %v", explosive, w.Events)
		}
		got := srv.InventoryAt(4, 64, 0)
		if servertest.NothingIn(got) || got[0].Count != 1 || got[0].Damage != 1 {
			t.Errorf("explosive=%v: dispenser holds %+v, want worn flint and steel", explosive, got)
		}
	}
}
