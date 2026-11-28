package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The equipment and honeycomb behaviours of a dispenser at (4, 64, 0)
// facing south: the cell in front is (4, 64, 1).

// equipDispense fires one item out of the dispenser and returns the items
// lying in the world after it, with the events the client heard.
func equipDispense(t *testing.T, item string, prepare func(srv *server.Server, w *servertest.WindowedClient)) (*server.Server, []server.TestItem, []int32) {
	t.Helper()
	srv, items, events, _ := equipDispenseHeard(t, item, prepare)
	return srv, items, events
}

// equipDispenseHeard is equipDispense with the sound events the client heard
// as well.
func equipDispenseHeard(t *testing.T, item string, prepare func(srv *server.Server, w *servertest.WindowedClient)) (*server.Server, []server.TestItem, []int32, []string) {
	t.Helper()
	srv, w := servertest.Dispensing(t)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, item, 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	if prepare != nil {
		prepare(srv, w)
	}
	w.DrainRecorded()
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	var sounds []string
	w.SettleAndCollect(func(id int32, data []byte) {
		w.Record(id, data)
		if id == protocol.IDPlaySoundEffect {
			if sound := protocol.NewReader(data).ReadVarInt(); sound > 0 {
				name, _ := servertest.SoundEventName(sound - 1)
				sounds = append(sounds, name)
			}
		}
	})
	return srv, srv.ItemsInDimension(world.DimensionName), w.Events, sounds
}

func countDropped(items []server.TestItem, name string) int {
	n := 0
	for _, it := range items {
		if it.Item == name {
			n += int(it.Count)
		}
	}
	return n
}

// standInCell walks the player into the cell the dispenser faces.
func standInCell(w *servertest.WindowedClient) {
	w.FacePlayer(4.5, 64, 1.5, 180, 0)
	w.DrainRecorded()
}

// TestADispenserDressesAPlayerInFront checks a chestplate lands on the
// chest slot of the player standing in the cell, with the click and no item
// on the ground.
// vanilla: EquipmentDispenseItemBehavior.dispenseEquipment
func TestADispenserDressesAPlayerInFront(t *testing.T) {
	t.Parallel()
	srv, items, events := equipDispense(t, "minecraft:iron_chestplate", func(_ *server.Server, w *servertest.WindowedClient) {
		standInCell(w)
	})
	want := registry.MustItem("minecraft:iron_chestplate")
	if got := srv.InventorySlot("Testificate", player.ArmorSlot+1); got.ItemID != want || got.Count != 1 {
		t.Errorf("the chest slot holds %+v, want the iron chestplate", got)
	}
	if n := countDropped(items, "minecraft:iron_chestplate"); n != 0 {
		t.Errorf("%d chestplates dropped, want the player to wear it", n)
	}
	if !slices.Contains(events, protocol.WorldEventDispense) {
		t.Errorf("no dispense click; events were %v", events)
	}
	if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
		t.Errorf("the dispenser keeps %+v, want it empty", left)
	}
}

// TestADispenserElytraAndShieldGoWhereTheirComponentSays checks the slot is
// the equippable component's, so an elytra takes the chest and a shield the
// off hand.
func TestADispenserElytraAndShieldGoWhereTheirComponentSays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		slot int
	}{
		{"minecraft:elytra", player.ArmorSlot + 1},
		{"minecraft:shield", player.OffHandSlot},
		{"minecraft:diamond_boots", player.ArmorSlot + 3},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := equipDispense(t, tc.item, func(_ *server.Server, w *servertest.WindowedClient) {
				standInCell(w)
			})
			if got := srv.InventorySlot("Testificate", tc.slot).ItemID; got != registry.MustItem(tc.item) {
				t.Errorf("slot %d holds item %d, want %s", tc.slot, got, tc.item)
			}
		})
	}
}

// TestADispenserDropsArmourForAWornSlot checks a slot already worn refuses
// the piece, which drops, the old one staying.
func TestADispenserDropsArmourForAWornSlot(t *testing.T) {
	t.Parallel()
	srv, items, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, w *servertest.WindowedClient) {
		srv.GiveSlot("Testificate", player.ArmorSlot, servertest.ItemStack(t, "minecraft:leather_helmet", 1))
		standInCell(w)
	})
	if got := srv.InventorySlot("Testificate", player.ArmorSlot).ItemID; got != registry.MustItem("minecraft:leather_helmet") {
		t.Errorf("the head holds item %d, want the leather helmet kept", got)
	}
	if n := countDropped(items, "minecraft:iron_helmet"); n != 1 {
		t.Errorf("%d helmets dropped, want 1", n)
	}
}

// TestADispenserDressesAMobThatTakesLoot checks a zombie that picks loot up
// is helmeted, kept alive with its guaranteed drop, while one that does not
// gets the piece on the ground.
// vanilla: Mob.canDispenserEquipIntoSlot, Mob.setGuaranteedDrop
func TestADispenserDressesAMobThatTakesLoot(t *testing.T) {
	t.Parallel()
	var zombie int32
	srv, items, events := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
		srv.SetMobCanPickUpLoot(zombie)
	})
	if got := srv.MobEquipment(zombie, "head"); got.ItemID != registry.MustItem("minecraft:iron_helmet") {
		t.Errorf("the zombie wears %+v, want the iron helmet", got)
	}
	if n := countDropped(items, "minecraft:iron_helmet"); n != 0 {
		t.Errorf("%d helmets dropped, want 0", n)
	}
	if !slices.Contains(events, protocol.WorldEventDispense) {
		t.Errorf("no dispense click; events were %v", events)
	}

	srv, items, _ = equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
	})
	if got := srv.MobEquipment(zombie, "head"); !got.Empty() {
		t.Errorf("the zombie without the loot flag wears %+v, want nothing", got)
	}
	if n := countDropped(items, "minecraft:iron_helmet"); n != 1 {
		t.Errorf("%d helmets dropped, want 1", n)
	}
}

// vanilla: AbstractHorse.canDispenserEquipIntoSlot, Mob.canDispenserEquipIntoSlot
func TestADispenserDressesEquinesInBodyEquipment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, item         string
		tame, loot, worn, equips bool
	}{
		{"tamed horse", "horse", "iron_horse_armor", true, false, false, true},
		{"looting wild horse", "horse", "iron_horse_armor", false, true, false, true},
		{"tamed llama", "llama", "white_carpet", true, false, false, true},
		{"horse refuses carpet", "horse", "white_carpet", true, true, false, false},
		{"llama refuses armor", "llama", "iron_horse_armor", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mob int32
			item := "minecraft:" + tc.item
			wornItem := "minecraft:leather_horse_armor"
			if tc.kind == "llama" {
				wornItem = "minecraft:red_carpet"
			}
			srv, items, _, sounds := equipDispenseHeard(t, item, func(srv *server.Server, _ *servertest.WindowedClient) {
				mob = srv.SpawnMob("minecraft:"+tc.kind, 4.5, 64, 1.5)
				if tc.tame {
					srv.TameHorse(mob)
				}
				if tc.loot {
					srv.SetMobCanPickUpLoot(mob)
				}
				if tc.worn {
					srv.SetMobEquipment(mob, "body", servertest.ItemStack(t, wornItem, 1))
				}
			})
			got := srv.MobEquipment(mob, "body")
			want := item
			if tc.worn {
				want = wornItem
			}
			if tc.equips || tc.worn {
				if got.ItemID != registry.MustItem(want) || got.Count != 1 {
					t.Errorf("body = %+v, want one %s", got, want)
				}
			} else if !got.Empty() {
				t.Errorf("body = %+v, want empty", got)
			}
			wantDropped := 1
			if tc.equips {
				wantDropped = 0
			}
			if n := countDropped(items, item); n != wantDropped {
				t.Errorf("dropped %d pieces, want %d", n, wantDropped)
			}
			equippable, _ := registry.StackEquippable(servertest.ItemStack(t, item, 1))
			if heard := slices.Contains(sounds, registry.Sound(equippable.EquipSound.ID+1).Name()); heard != tc.equips {
				t.Errorf("equip sound heard = %v, want %v; sounds = %v", heard, tc.equips, sounds)
			}
		})
	}
}

// TestADispenserDressesAnArmorStand checks a stand takes the piece for a
// bare slot and the next piece for that slot drops.
// vanilla: ArmorStand.canUseSlot
func TestADispenserDressesAnArmorStand(t *testing.T) {
	t.Parallel()
	var stand int32
	srv, items, _ := equipDispense(t, "minecraft:iron_leggings", func(srv *server.Server, _ *servertest.WindowedClient) {
		stand = srv.SpawnArmorStand(4.5, 64, 1.5)
	})
	if got := srv.ArmorStandStack(stand, entity.StandLegs); got.ItemID != registry.MustItem("minecraft:iron_leggings") {
		t.Errorf("the stand's legs hold %+v, want the iron leggings", got)
	}
	if n := countDropped(items, "minecraft:iron_leggings"); n != 0 {
		t.Errorf("%d leggings dropped, want 0", n)
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:iron_leggings", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:iron_leggings"); n != 1 {
		t.Errorf("%d leggings dropped by the second shot, want 1", n)
	}
}

// TestADispenserWaxesTheCopperInFront checks a honeycomb waxes the block in
// the cell with its properties kept, plays the wax-on event, and is spent.
// vanilla: HoneycombItem.getWaxed
func TestADispenserWaxesTheCopperInFront(t *testing.T) {
	t.Parallel()
	stairs, ok := registry.FindState("minecraft:weathered_cut_copper_stairs", map[string]string{"facing": "west", "half": "top"})
	if !ok {
		t.Fatal("no such stairs state")
	}
	srv, items, events, sounds := equipDispenseHeard(t, "minecraft:honeycomb", func(srv *server.Server, _ *servertest.WindowedClient) {
		srv.SetBlock(4, 64, 1, stairs)
	})
	name, props := servertest.StateProps(srv.BlockAt(4, 64, 1))
	if name != "minecraft:waxed_weathered_cut_copper_stairs" || props["facing"] != "west" || props["half"] != "top" {
		t.Errorf("the block is %s %v, want the waxed stairs with its properties", name, props)
	}
	if !slices.Contains(events, protocol.WorldEventWaxOn) {
		t.Errorf("no wax-on event; events were %v", events)
	}
	if !slices.Contains(sounds, "minecraft:item.honeycomb.wax_on") {
		t.Errorf("no wax-on sound; sounds were %v", sounds)
	}
	if n := countDropped(items, "minecraft:honeycomb"); n != 0 {
		t.Errorf("%d honeycombs dropped, want it spent", n)
	}
	if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
		t.Errorf("the dispenser keeps %+v, want it empty", left)
	}
}

// TestADispenserBuildsAGolemWithItsCarvedPumpkin checks the pumpkin is set
// into the air cell in front when a golem base stands near it.
func TestADispenserBuildsAGolemWithItsCarvedPumpkin(t *testing.T) {
	t.Parallel()
	t.Run("snow column", func(t *testing.T) {
		t.Parallel()
		snow := servertest.BlockState(t, "minecraft:snow_block")
		srv, items, events := equipDispense(t, "minecraft:carved_pumpkin", func(srv *server.Server, _ *servertest.WindowedClient) {
			srv.SetBlock(4, 63, 1, snow)
			srv.SetBlock(4, 62, 1, snow)
		})
		mobs := srv.Mobs()
		if len(mobs) != 1 || mobs[0].Kind != "minecraft:snow_golem" {
			t.Errorf("mobs = %+v, want one snow golem", mobs)
		}
		for _, y := range []int32{64, 63, 62} {
			if b := srv.BlockAt(4, y, 1); b != 0 {
				t.Errorf("block at y=%d is %d, want air", y, b)
			}
		}
		if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
			t.Errorf("the dispenser keeps %+v, want it empty", left)
		}
		if n := countDropped(items, "minecraft:carved_pumpkin"); n != 0 {
			t.Errorf("%d pumpkins dropped", n)
		}
		if !slices.Contains(events, protocol.WorldEventDispense) || slices.Contains(events, protocol.WorldEventDispenseFail) {
			t.Errorf("events were %v, want the success click", events)
		}
	})
	t.Run("copper diagonal", func(t *testing.T) {
		t.Parallel()
		copper := servertest.BlockState(t, "minecraft:copper_block")
		srv, _, events := equipDispense(t, "minecraft:carved_pumpkin", func(srv *server.Server, _ *servertest.WindowedClient) {
			srv.SetBlock(5, 63, 1, copper)
		})
		if name, _ := servertest.StateProps(srv.BlockAt(4, 64, 1)); name != "minecraft:carved_pumpkin" {
			t.Errorf("the cell holds %s, want the carved pumpkin", name)
		}
		if mobs := srv.Mobs(); len(mobs) != 0 {
			t.Errorf("mobs = %+v, want none", mobs)
		}
		if srv.BlockAt(5, 63, 1) != copper {
			t.Error("the copper block is gone")
		}
		if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
			t.Errorf("the dispenser keeps %+v, want it empty", left)
		}
		if !slices.Contains(events, protocol.WorldEventDispense) {
			t.Errorf("events were %v, want the success click", events)
		}
	})
	t.Run("no base", func(t *testing.T) {
		t.Parallel()
		srv, _, events := equipDispense(t, "minecraft:carved_pumpkin", nil)
		if b := srv.BlockAt(4, 64, 1); b != 0 {
			t.Errorf("the cell holds %d, want air", b)
		}
		if left := srv.InventoryAt(4, 64, 0)[0]; left.Empty() {
			t.Error("the pumpkin left the dispenser")
		}
		if !slices.Contains(events, protocol.WorldEventDispenseFail) {
			t.Errorf("events were %v, want the fail click", events)
		}
	})
}

// TestADispenserDressesAMannequin checks a mannequin takes the piece for a
// bare slot and the piece is not dropped.
// vanilla: EquipmentDispenseItemBehavior.dispenseEquipment, LivingEntity.canEquipWithDispenser
func TestADispenserDressesAMannequin(t *testing.T) {
	t.Parallel()
	var mannequin int32
	srv, items, _ := equipDispense(t, "minecraft:diamond_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		mannequin = srv.SpawnMannequinForTest(4.5, 64, 1.5)
	})
	if got := srv.MannequinHeadForTest(mannequin); got.ItemID != registry.MustItem("minecraft:diamond_helmet") {
		t.Errorf("the mannequin's head holds %+v, want the diamond helmet", got)
	}
	if n := countDropped(items, "minecraft:diamond_helmet"); n != 0 {
		t.Errorf("%d helmets dropped, want 0", n)
	}
}
