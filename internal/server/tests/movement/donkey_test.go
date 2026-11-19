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
