package redstone

import (
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
