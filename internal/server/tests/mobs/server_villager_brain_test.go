package mobs

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestAJoblessVillagerTakesALecternTwentySixBlocksAway: the path to a job
// site beyond the follow range of 16 is searched to the villager's required
// path length of 48.
// vanilla: Villager.<init> setRequiredPathLength, PathNavigation.getMaxPathLength
func TestAJoblessVillagerTakesALecternTwentySixBlocksAway(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(2000)
	srv.SetBlock(26, 64, 0, stateOf(t, "minecraft:lectern", map[string]string{"facing": "north"}))
	srv.SpawnMob("minecraft:villager", 1.5, 64, 0.5)
	seen := c.WatchVariant(srv, "minecraft:villager")
	for range 60 {
		advanceVillager(t, srv, c, 10, &seen)
		if data := seen.Meta[protocol.MetadataIndexVillagerVillagerData]; len(data) == 3 && data[1] == professionLibrarian {
			return
		}
	}
	t.Errorf("villager data after 600 ticks %v, want the librarian %d", seen.Meta[protocol.MetadataIndexVillagerVillagerData], professionLibrarian)
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

// TestAVillagerAsleepWhenItsChunkUnloadsIsStillInBedAndFreesItByDay saves a
// sleeping villager with its bed, reloads it, and finds the bed still
// occupied at night and freed once day comes.
// vanilla: LivingEntity.addAdditionalSaveData, readAdditionalSaveData
func TestAVillagerAsleepWhenItsChunkUnloadsIsStillInBedAndFreesItByDay(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Herder"))
	c := connect().Join("Herder")
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(12500)
	srv.SetBlock(3, 64, 0, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(2, 64, 0, servertest.BedStateName("red", "foot", "east"))
	srv.SpawnMob("minecraft:villager", 1.5, 64, 2.5)
	occupied := func() bool { return registry.StateBool(srv.BlockAt(3, 64, 0), registry.PropOccupied) }
	servertest.UntilTicks(t, srv, 600, "the villager in bed", occupied)

	c.Disconnect()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:villager", func(compound nbt.Compound) {
		if got, _ := nbt.Get[nbt.IntArray](compound, "sleeping_pos"); !slices.Equal(got, nbt.IntArray{3, 64, 0}) {
			t.Errorf("saved sleeping_pos = %v, want the bed head [3 64 0]", compound["sleeping_pos"])
		}
	})

	connect().Join("Herder")
	servertest.WaitFor(t, srv, 200, func() bool { return srv.MobCount() == 1 })
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 40)
	if !occupied() {
		t.Fatal("the bed is free 40 ticks after the sleeper reloaded")
	}
	srv.SetDayTime(0)
	servertest.RunTicks(t, srv, 120)
	if occupied() {
		t.Error("the bed is still occupied 120 ticks into the day")
	}
}

// vanilla: Villager.getAmbientSound, WanderingTrader.getAmbientSound
func TestMerchantsUseTheirTradeVoiceWhileTheirWindowIsOpen(t *testing.T) {
	for _, kind := range []string{"villager", "wandering_trader"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := villagerYard(t, servertest.OpsListing(t, "Villager"))
			srv.SetDayTime(2000)
			c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 1.5 64 0.5 {NoAI:1b,PersistenceRequired:1b,VillagerData:{profession:"minecraft:librarian",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:1},maxUses:99}]}}`, kind))
			seen := c.WatchVariant(srv, "minecraft:"+kind)
			c.UseOn(seen.EntityID)
			payload := c.ExpectWithinTicks(srv, 0, protocol.IDPlayMerchantOffers)
			window := protocol.NewReader(payload).ReadVarInt()
			servertest.CollectSoundsSettled(c)
			heard, _, _ := servertest.CollectSoundsTicks(c, srv, 1200)
			trade, ambient := "minecraft:entity."+kind+".trade", "minecraft:entity."+kind+".ambient"
			trades := 0
			for _, sound := range heard {
				if sound.Name == ambient {
					t.Errorf("merchant with an open window played %s", ambient)
				}
				if sound.Name == trade {
					trades++
				}
			}
			if trades == 0 {
				t.Errorf("no %s over 1200 ticks", trade)
			}
			c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) { b.PutVarInt(window) })
			c.Settle()
			heard, _, _ = servertest.CollectSoundsTicks(c, srv, 1200)
			ambients := 0
			for _, sound := range heard {
				if sound.Name == trade {
					t.Errorf("merchant with a closed window played %s", trade)
				}
				if sound.Name == ambient {
					ambients++
				}
			}
			if ambients == 0 {
				t.Errorf("no %s after closing the window", ambient)
			}
		})
	}
}
