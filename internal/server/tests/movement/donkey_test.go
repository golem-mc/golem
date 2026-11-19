package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The donkey and the mule as the wire shows them: their spawn, the chest a
// tame one takes and the saddle.
// vanilla: Donkey, Mule, AbstractChestedHorse

// equineSummon stands up a donkey or mule next to a player with the tag given
// and returns its entity ID, with the server's tick stopped.
func equineSummon(t *testing.T, kind, tag string) (rider *servertest.Client, id int32, srv *server.Server) {
	t.Helper()
	s, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider = connect()
	rider.Join("Rider")
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	rider.ChatCommand("summon " + kind + " 5.5 64 5.5 " + tag)
	want, ok := registry.EntityTypeID(kind)
	if !ok {
		t.Fatalf("the entity registry has no %s", kind)
	}
	for pid, data := range rider.Packets(servertest.ExpectTimeout) {
		if pid != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == want {
			return rider, entityID, s
		}
	}
	t.Fatalf("no %s spawned", kind)
	return nil, 0, nil
}

// chestFlag reads the chest boolean out of a Set Entity Metadata packet, -1
// when the packet does not carry it.
func chestFlag(data []byte) int {
	r := protocol.NewReader(data)
	r.ReadVarInt()
	chest := -1
	for {
		index := r.ReadU8()
		if index == protocol.MetadataEnd || r.Err() != nil {
			return chest
		}
		switch r.ReadVarInt() {
		case protocol.MetadataByte:
			r.ReadU8()
		case protocol.MetadataVarInt:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			v := r.ReadBool()
			if index == protocol.MetadataIndexChestedHorseChest && v {
				chest = 1
			} else if index == protocol.MetadataIndexChestedHorseChest {
				chest = 0
			}
		default:
			return chest
		}
	}
}

func giveEquineItem(srv *server.Server, rider *servertest.Client, name string) {
	id, _ := registry.ItemID(name)
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	rider.Settle()
}

// TestDonkeyAndMuleSpawnCarryingTheirChest: a summoned donkey or mule with
// ChestedHorse set tells the chest flag, and a horse has no such field.
func TestDonkeyAndMuleSpawnCarryingTheirChest(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:donkey", "minecraft:mule"} {
		rider, _, _ := equineSummon(t, kind, "{Tame:1b,ChestedHorse:1b}")
		chest := -1
		for id, data := range rider.Packets(servertest.ExpectTimeout) {
			if id == protocol.IDPlayEntityMetadata {
				if chest = chestFlag(data); chest >= 0 {
					break
				}
			}
		}
		if chest != 1 {
			t.Errorf("%s spawned with chest flag %d, want 1", kind, chest)
		}
	}
}

// TestAChestOnATameDonkeyIsFittedAndSpent: a chest used on a tame donkey or
// mule sets the chest flag and is spent from the hand.
func TestAChestOnATameDonkeyIsFittedAndSpent(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:donkey", "minecraft:mule"} {
		rider, id, srv := equineSummon(t, kind, "{Tame:1b}")
		giveEquineItem(srv, rider, "minecraft:chest")
		rider.UseOn(id)
		chest := -1
		record := func(pid int32, data []byte) {
			if pid == protocol.IDPlayEntityMetadata {
				if c := chestFlag(data); c >= 0 {
					chest = c
				}
			}
		}
		rider.SettleAndCollect(record)
		srv.TickMobs(1)
		rider.SettleAndCollect(record)
		if chest != 1 {
			t.Errorf("%s: chest flag after the click %d, want 1", kind, chest)
		}
		if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 0 {
			t.Errorf("%s: hand holds %d chests after fitting one, want 0", kind, got.Count)
		}
	}
}

// TestAChestOnAWildDonkeyRearsAndIsNotFitted: an untamed donkey takes no chest
// and keeps it in the hand.
func TestAChestOnAWildDonkeyRearsAndIsNotFitted(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:donkey", "{}")
	giveEquineItem(srv, rider, "minecraft:chest")
	rider.UseOn(id)
	rider.Settle()
	srv.TickMobs(1)
	rider.Settle()
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 1 {
		t.Errorf("hand holds %d chests after a wild donkey's click, want 1", got.Count)
	}
}

// TestSaddlingATameMuleTellsTheSaddle: a saddle fits a tame mule in the
// saddle's equipment slot 7, as on a horse.
func TestSaddlingATameMuleTellsTheSaddle(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:mule", "{Tame:1b}")
	giveEquineItem(srv, rider, "minecraft:saddle")
	rider.UseOn(id)
	slot := -1
	record := func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id {
			slot = int(r.ReadU8())
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if slot != 7 {
		t.Errorf("saddle told in slot %d, want 7", slot)
	}
}

// TestACreativePlayersChestIsNotSpent: fitting a chest or a saddle in
// creative mode leaves the item in the hand.
// vanilla: ItemStack.consume
func TestACreativePlayersChestIsNotSpent(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:donkey", "{Tame:1b}")
	srv.SetPlayerGameMode("Rider", protocol.GameModeCreative)
	for _, item := range []string{"minecraft:chest", "minecraft:saddle"} {
		giveEquineItem(srv, rider, item)
		rider.UseOn(id)
		rider.Settle()
		srv.TickMobs(1)
		rider.Settle()
		if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 1 {
			t.Errorf("hand holds %d %s after the click in creative, want 1", got.Count, item)
		}
	}
}

// TestABabyDonkeysChestDropsWithMobDropsOff: the chest comes out on any
// death, a foal's and a world with mob_drops off included.
// vanilla: LivingEntity.dropAllDeathLoot, AbstractChestedHorse.dropEquipment
func TestABabyDonkeysChestDropsWithMobDropsOff(t *testing.T) {
	t.Parallel()
	rider, _, srv := equineSummon(t, "minecraft:donkey", "{Tame:1b,ChestedHorse:1b,Age:-24000}")
	rider.ChatCommand("gamerule mob_drops false")
	rider.Settle()
	rider.ChatCommand("kill @e[type=minecraft:donkey]")
	rider.Settle()
	srv.TickMobs(2)
	rider.Settle()
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:chest")
}
