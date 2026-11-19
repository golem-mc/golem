package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// villagerYard joins a player and holds the loop.
func villagerYard(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", "Villager", "", 0, setup...)
	servertest.HoldTicks(t, srv)
	return srv, c
}

// advanceVillager runs n ticks and folds the metadata the villager drew into
// seen.
func advanceVillager(t *testing.T, srv *server.Server, c *servertest.Client, n int, seen *servertest.VariantSeen) {
	t.Helper()
	servertest.RunTicks(t, srv, n)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == seen.EntityID {
			c.ReadVariantMetadata(r, seen)
		}
	})
}

// Villager professions' registry IDs.
const professionLibrarian = 9

// TestAJoblessVillagerBesideALecternBecomesALibrarian: it takes the lectern
// as its potential job site, walks up to it and takes the profession.
// vanilla: AcquirePoi, GoToPotentialJobSite, AssignProfessionFromJobSite
func TestAJoblessVillagerBesideALecternBecomesALibrarian(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(2000)
	srv.SetBlock(3, 64, 0, stateOf(t, "minecraft:lectern", map[string]string{"facing": "north"}))
	srv.SpawnMob("minecraft:villager", 1.5, 64, 0.5)
	seen := c.WatchVariant(srv, "minecraft:villager")
	if data := seen.Meta[protocol.MetadataIndexVillagerVillagerData]; len(data) != 3 || data[1] != 0 {
		t.Fatalf("villager data at the spawn %v, want no profession", data)
	}
	for range 20 {
		advanceVillager(t, srv, c, 10, &seen)
		if data := seen.Meta[protocol.MetadataIndexVillagerVillagerData]; len(data) == 3 && data[1] == professionLibrarian {
			return
		}
	}
	t.Errorf("villager data after 200 ticks %v, want the librarian %d", seen.Meta[protocol.MetadataIndexVillagerVillagerData], professionLibrarian)
}

// TestAVillagerSleepsInItsBedAtNightAndWakesInTheMorning: at night it walks
// to the bed, lies in it (pose, bed position, occupied bed), and by day it
// gets up and frees the bed.
// vanilla: SleepInBed, WakeUp, LivingEntity.startSleeping, stopSleeping
func TestAVillagerSleepsInItsBedAtNightAndWakesInTheMorning(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(12500)
	srv.SetBlock(3, 64, 0, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(2, 64, 0, servertest.BedStateName("red", "foot", "east"))
	srv.SpawnMob("minecraft:villager", 1.5, 64, 2.5)
	seen := c.WatchVariant(srv, "minecraft:villager")
	occupied := func() bool { return registry.StateBool(srv.BlockAt(3, 64, 0), registry.PropOccupied) }

	for range 30 {
		advanceVillager(t, srv, c, 10, &seen)
		if seen.Meta[protocol.MetadataIndexPose] != nil && seen.Meta[protocol.MetadataIndexPose][0] == protocol.PoseSleeping {
			break
		}
	}
	if pose := seen.Meta[protocol.MetadataIndexPose]; len(pose) != 1 || pose[0] != protocol.PoseSleeping {
		t.Fatalf("pose %v after 300 ticks of night beside a bed, want sleeping", pose)
	}
	if pos := seen.Meta[protocol.MetadataIndexSleepingPos]; len(pos) != 3 || pos[0] != 3 || pos[1] != 64 || pos[2] != 0 {
		t.Errorf("sleeping position %v, want the bed head 3 64 0", pos)
	}
	if !occupied() {
		t.Error("the bed the villager lies in is not occupied")
	}

	servertest.CollectSoundsSettled(c)
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 1200)
	for _, sound := range heard {
		if sound.Name == "minecraft:entity.villager.ambient" || sound.Name == "minecraft:entity.villager.trade" {
			t.Errorf("sleeping villager played %s", sound.Name)
		}
	}
	if !occupied() {
		t.Fatal("villager left its bed during the sound observation")
	}

	srv.SetDayTime(0)
	for range 12 {
		advanceVillager(t, srv, c, 10, &seen)
		if !occupied() {
			break
		}
	}
	if occupied() {
		t.Error("the bed is still occupied 120 ticks into the day")
	}
	if pose := seen.Meta[protocol.MetadataIndexPose]; len(pose) != 1 || pose[0] == protocol.PoseSleeping {
		t.Errorf("pose %v after waking, want standing", pose)
	}
	if pos := seen.Meta[protocol.MetadataIndexSleepingPos]; len(pos) != 0 {
		t.Errorf("sleeping position %v after waking, want none", pos)
	}
}

// TestAVillagerAtItsJobSiteRestocksItsTrades: at work it refills the offers
// it used and raises their demand.
// vanilla: WorkAtPoi, Villager.restock
func TestAVillagerAtItsJobSiteRestocksItsTrades(t *testing.T) {
	t.Parallel()
	// Work goals use game time as well as the mob's seeded random stream.
	srv, c := villagerYard(t, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.SetDayTime(3000)
	srv.SetBlock(3, 64, 0, stateOf(t, "minecraft:lectern", map[string]string{"facing": "north"}))
	recipe := nbt.Compound{
		"buy":             nbt.Compound{"id": nbt.String("minecraft:paper"), "count": nbt.Int(24)},
		"sell":            nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
		"uses":            nbt.Int(12),
		"maxUses":         nbt.Int(16),
		"xp":              nbt.Int(2),
		"priceMultiplier": nbt.Float(0.05),
	}
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
	id := srv.SpawnVillagerAtJobSite("minecraft:librarian", offers, 2.5, 64, 0.5, 3, 64, 0)
	seen := servertest.VariantSeen{EntityID: id, Meta: map[uint8][]int32{}}

	// WorkAtPoi rolls once per 300 ticks, at even odds, and only by the site:
	// 1200 ticks give it four tries within the work period.
	for range 120 {
		advanceVillager(t, srv, c, 10, &seen)
		saved, _ := srv.VillagerSaved(id)
		list, _ := nbt.Get[nbt.List](saved, "Recipes")
		uses, _ := nbt.IntField(list.Items[0].(nbt.Compound), "uses")
		if uses == 0 {
			break
		}
	}
	c.UseOn(id)
	var payload []byte
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayMerchantOffers {
			payload = data
		}
	})
	if payload == nil {
		t.Fatal("no merchant offers after the villager worked")
	}
	r := protocol.NewReader(payload)
	r.ReadVarInt() // window
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("%d offers, want 1", n)
	}
	r.ReadVarInt() // cost item
	r.ReadVarInt() // cost count
	r.ReadVarInt() // predicate
	r.ReadSlot()
	r.ReadBool() // costB
	r.ReadBool() // out of stock
	uses, maxUses, _, special := r.ReadI32(), r.ReadI32(), r.ReadI32(), r.ReadI32()
	r.ReadF32()
	demand := r.ReadI32()
	if uses != 0 || maxUses != 16 || demand != 12-(16-12) {
		t.Errorf("uses %d, max %d, demand %d (special %d), want 0, 16 and 8", uses, maxUses, demand, special)
	}
}
