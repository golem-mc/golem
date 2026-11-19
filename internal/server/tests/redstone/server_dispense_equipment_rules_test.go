package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// headSlot is the head's place in server.MobGear's slots.
const headSlot = 5

// TestADispenserDressedMobKeepsTheItemAndItsDrop checks a mob that took a
// helmet is kept from despawning and drops the helmet for certain.
// vanilla: Mob.setGuaranteedDrop, Mob.setPersistenceRequired
func TestADispenserDressedMobKeepsTheItemAndItsDrop(t *testing.T) {
	t.Parallel()
	var zombie int32
	srv, _, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
		srv.SetMobCanPickUpLoot(zombie)
	})
	if !srv.MobPersistent(zombie) {
		t.Error("the dressed zombie may still despawn")
	}
	if _, chances := srv.MobGear(zombie); chances[headSlot] <= 1 {
		t.Errorf("the helmet drops with chance %v, want a certain drop", chances[headSlot])
	}
}

// TestADispenserRefusesWolfArmorLookalikes checks a wolf that picks loot up
// takes only the items that allow a wolf: horse armor, a llama carpet and a
// happy ghast harness are dropped instead.
// vanilla: Equippable.canBeEquippedBy
func TestADispenserRefusesWolfArmorLookalikes(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"minecraft:leather_horse_armor", "minecraft:white_carpet", "minecraft:white_harness"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			var wolf int32
			srv, items, _ := equipDispense(t, item, func(srv *server.Server, _ *servertest.WindowedClient) {
				wolf = srv.SpawnMob("minecraft:wolf", 4.5, 64, 1.5)
				srv.SetMobCanPickUpLoot(wolf)
			})
			if got := srv.MobEquipment(wolf, "body"); !got.Empty() {
				t.Errorf("the wolf wears %+v, want nothing", got)
			}
			if n := countDropped(items, item); n != 1 {
				t.Errorf("%d dropped, want 1", n)
			}
		})
	}
}

// TestADispenserKeepsAHeadPieceNobodyWears checks a carved pumpkin or wither
// skull with no wearer stays in the dispenser with the fail click, where an
// iron helmet is dropped.
// vanilla: DispenseItemBehavior (carved pumpkin, wither skeleton skull)
func TestADispenserKeepsAHeadPieceNobodyWears(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"minecraft:carved_pumpkin", "minecraft:wither_skeleton_skull"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			srv, items, events := equipDispense(t, item, nil)
			if got := srv.InventoryAt(4, 64, 0)[0]; got.ItemID != registry.MustItem(item) || got.Count != 1 {
				t.Errorf("the dispenser holds %+v, want the item kept", got)
			}
			if n := countDropped(items, item); n != 0 {
				t.Errorf("%d dropped, want 0", n)
			}
			if !slices.Contains(events, protocol.WorldEventDispenseFail) || slices.Contains(events, protocol.WorldEventDispense) {
				t.Errorf("events %v, want the fail click alone", events)
			}
		})
	}
	var zombie int32
	srv, _, _ := equipDispense(t, "minecraft:carved_pumpkin", func(srv *server.Server, _ *servertest.WindowedClient) {
		zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
		srv.SetMobCanPickUpLoot(zombie)
	})
	if got := srv.MobEquipment(zombie, "head"); got.ItemID != registry.MustItem("minecraft:carved_pumpkin") {
		t.Errorf("the zombie wears %+v, want the pumpkin", got)
	}
}

// TestADispenserDressesTheFirstBodyByEntityID checks that of two bodies in
// the cell the one made first wears the piece, whatever their kinds, however
// often it is fired.
// vanilla: EquipmentDispenseItemBehavior.dispenseEquipment takes the first entity
func TestADispenserDressesTheFirstBodyByEntityID(t *testing.T) {
	t.Parallel()
	for run := range 12 {
		var first, second int32
		srv, _, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
			first = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
			second = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
			srv.SetMobCanPickUpLoot(first)
			srv.SetMobCanPickUpLoot(second)
		})
		if srv.MobEquipment(first, "head").Empty() || !srv.MobEquipment(second, "head").Empty() {
			t.Fatalf("run %d: the helmet went to the later zombie", run)
		}
	}
	for _, standFirst := range []bool{true, false} {
		var stand, zombie int32
		srv, _, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
			spawnStand := func() { stand = srv.SpawnArmorStand(4.5, 64, 1.5) }
			spawnZombie := func() {
				zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
				srv.SetMobCanPickUpLoot(zombie)
			}
			if standFirst {
				spawnStand()
				spawnZombie()
			} else {
				spawnZombie()
				spawnStand()
			}
		})
		onStand := !srv.ArmorStandStack(stand, entity.StandHead).Empty()
		onZombie := !srv.MobEquipment(zombie, "head").Empty()
		if onStand == onZombie || onStand != standFirst {
			t.Errorf("stand first %v: helmet on the stand %v, on the zombie %v", standFirst, onStand, onZombie)
		}
	}
}

// TestADispenserSaddlesATamedHorseWithoutTheLootFlag checks a tamed horse
// takes a saddle with no loot flag, and a wild one does not.
// vanilla: AbstractHorse.canDispenserEquipIntoSlot
func TestADispenserSaddlesATamedHorseWithoutTheLootFlag(t *testing.T) {
	t.Parallel()
	for _, tame := range []bool{true, false} {
		var horse int32
		srv, items, _ := equipDispense(t, "minecraft:saddle", func(srv *server.Server, _ *servertest.WindowedClient) {
			horse = srv.SpawnMob("minecraft:horse", 4.5, 64, 1.5)
			if tame {
				srv.TameHorse(horse)
			}
		})
		saddled := !srv.MobEquipment(horse, "saddle").Empty()
		dropped := countDropped(items, "minecraft:saddle")
		if saddled != tame || (dropped == 1) != !tame {
			t.Errorf("tame %v: saddled %v, saddles dropped %d", tame, saddled, dropped)
		}
	}
}

// TestADispenserPlaysTheEquipSound checks the dressed body's piece sounds its
// own equip sound, and that a piece nobody wears is silent.
// vanilla: LivingEntity.onEquipItem
func TestADispenserPlaysTheEquipSound(t *testing.T) {
	t.Parallel()
	_, _, _, sounds := equipDispenseHeard(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		srv.SpawnArmorStand(4.5, 64, 1.5)
	})
	if !slices.Contains(sounds, "minecraft:item.armor.equip_iron") {
		t.Errorf("sounds %v, want the iron equip sound", sounds)
	}
	_, _, _, sounds = equipDispenseHeard(t, "minecraft:wolf_armor", func(srv *server.Server, _ *servertest.WindowedClient) {
		srv.SetMobCanPickUpLoot(srv.SpawnMob("minecraft:wolf", 4.5, 64, 1.5))
	})
	if !slices.Contains(sounds, "minecraft:item.armor.equip_wolf") {
		t.Errorf("sounds %v, want the wolf armor equip sound", sounds)
	}
	_, _, _, sounds = equipDispenseHeard(t, "minecraft:iron_helmet", nil)
	if slices.Contains(sounds, "minecraft:item.armor.equip_iron") {
		t.Errorf("sounds %v, want no equip sound with nobody dressed", sounds)
	}
}

// TestADispenserSaddleSoundIsTheMobs checks a strider's saddle plays the
// strider sound and a tamed horse's the horse sound.
// vanilla: Strider.getEquipSound, AbstractHorse.getEquipSound
func TestADispenserSaddleSoundIsTheMobs(t *testing.T) {
	t.Parallel()
	_, _, _, sounds := equipDispenseHeard(t, "minecraft:saddle", func(srv *server.Server, _ *servertest.WindowedClient) {
		srv.SpawnMob("minecraft:strider", 4.5, 64, 1.5)
	})
	if !slices.Contains(sounds, "minecraft:entity.strider.saddle") || slices.Contains(sounds, "minecraft:entity.horse.saddle") {
		t.Errorf("strider sounds %v, want the strider saddle sound alone", sounds)
	}
	_, _, _, sounds = equipDispenseHeard(t, "minecraft:saddle", func(srv *server.Server, _ *servertest.WindowedClient) {
		srv.TameHorse(srv.SpawnMob("minecraft:horse", 4.5, 64, 1.5))
	})
	if !slices.Contains(sounds, "minecraft:entity.horse.saddle") {
		t.Errorf("horse sounds %v, want the horse saddle sound", sounds)
	}
}

// TestADispenserEquipWakesASculkSensor checks the equip game event reaches a
// sensor three blocks from the dressed stand, and that a dropped piece
// raises none.
// vanilla: LivingEntity.onEquipItem (GameEvent.EQUIP)
func TestADispenserEquipWakesASculkSensor(t *testing.T) {
	t.Parallel()
	for _, dressed := range []bool{true, false} {
		srv, _, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
			srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			if dressed {
				srv.SpawnArmorStand(4.5, 64, 1.5)
			}
		})
		servertest.HoldTicks(t, srv)
		servertest.RunTicks(t, srv, 4)
		phase := "inactive"
		if dressed {
			phase = "active"
		}
		name, props := servertest.StatePropsOf(t, srv, 4, 64, 4)
		if name != "minecraft:sculk_sensor" || props["sculk_sensor_phase"] != phase {
			t.Errorf("dressed %v: sensor %s %v, want phase %s", dressed, name, props, phase)
		}
	}
}

// TestADispenserDressesAWideMobWhoseCentreIsOutsideTheCell checks a ghast
// whose centre is 2.4 blocks off the cell's, its 4-block box reaching in,
// takes the piece.
// vanilla: EquipmentDispenseItemBehavior.dispenseEquipment (AABB query)
func TestADispenserDressesAWideMobWhoseCentreIsOutsideTheCell(t *testing.T) {
	t.Parallel()
	var ghast int32
	srv, _, _ := equipDispense(t, "minecraft:iron_helmet", func(srv *server.Server, _ *servertest.WindowedClient) {
		ghast = srv.SpawnMob("minecraft:ghast", 6.9, 64, 1.5)
		srv.SetMobCanPickUpLoot(ghast)
	})
	if got := srv.MobEquipment(ghast, "head"); got.Empty() {
		t.Error("the ghast beside the cell was not dressed")
	}
}
