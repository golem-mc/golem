package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The location and item triggers: what the player sets down or uses on a
// block, the horse they tame and the effects they carry.

// TestEveryPotionEffectObtainsAllPotions: effects_changed passes once the
// player carries the whole list, and not a short one.
// vanilla: ServerPlayer.onEffectAdded, EffectsChangedTrigger
func TestEveryPotionEffectObtainsAllPotions(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Brewer")
	var names []string
	for _, a := range registry.Advancements {
		if a.ID == "minecraft:nether/all_potions" {
			names = a.Criteria[0].Effects
		}
	}
	if len(names) < 2 {
		t.Fatal("all_potions lists no effects")
	}
	for _, name := range names[:len(names)-1] {
		srv.GiveEffect("Brewer", name, 6000, 0, true)
	}
	if srv.AdvancementObtained("Brewer", "minecraft:nether/all_potions", "all_effects") {
		t.Fatal("all but one effect obtained all_potions")
	}
	srv.GiveEffect("Brewer", names[len(names)-1], 6000, 0, true)
	if !srv.AdvancementObtained("Brewer", "minecraft:nether/all_potions", "all_effects") {
		t.Error("every effect did not obtain all_potions")
	}
}

// TestRidingAWildHorseToTameObtainsTameAnimal: a rider whose bucking horse
// gives in has tamed it. vanilla: RunAroundLikeCrazyGoal.tick, AbstractHorse.tameWithName
func TestRidingAWildHorseToTameObtainsTameAnimal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	// A temper of 99 of 100 gives in at nearly every roll.
	c.ChatCommand("summon minecraft:horse 5.5 64 5.5 {Temper:99}")
	var id int32
	for pid, data := range c.Packets(servertest.ExpectTimeout) {
		if pid != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		eid := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeHorse {
			id = eid
			break
		}
	}
	if id == 0 {
		t.Fatal("no horse spawned")
	}
	c.UseOn(id)
	c.Settle()
	if srv.AdvancementObtained("Rider", "minecraft:husbandry/tame_an_animal", "tamed_animal") {
		t.Fatal("mounting alone obtained tame_an_animal")
	}
	servertest.UntilTicks(t, srv, 2000, "the horse is tamed", func() bool {
		return srv.AdvancementObtained("Rider", "minecraft:husbandry/tame_an_animal", "tamed_animal")
	})
}

// TestBuildingAnIronGolemObtainsSummonIronGolem: a carved pumpkin on an iron
// T near the player is a summoned_entity for the iron golem; a snow golem is
// not. vanilla: CarvedPumpkinBlock.trySpawnGolem, SummonedEntityTrigger
func TestBuildingAnIronGolemObtainsSummonIronGolem(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	srv.GiveSlot("Builder", player.HotbarSlot, servertest.ItemStack(t, "minecraft:carved_pumpkin", 2))
	c.Settle()
	snow := servertest.MustState(t, "minecraft:snow_block", nil)
	srv.SetBlock(2, 64, 3, snow)
	srv.SetBlock(2, 65, 3, snow)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	c.Settle()
	if srv.AdvancementObtained("Builder", "minecraft:adventure/summon_iron_golem", "summoned_golem") {
		t.Fatal("a snow golem obtained summon_iron_golem")
	}
	iron := servertest.MustState(t, "minecraft:iron_block", nil)
	for _, at := range [][3]int32{{-3, 64, 3}, {-4, 65, 3}, {-3, 65, 3}, {-2, 65, 3}} {
		srv.SetBlock(at[0], at[1], at[2], iron)
	}
	c.PlaceBlock(-3, 65, 3, protocol.FaceTop)
	c.Settle()
	if !srv.AdvancementObtained("Builder", "minecraft:adventure/summon_iron_golem", "summoned_golem") {
		t.Error("an iron golem did not obtain summon_iron_golem")
	}
}

// doneWithinTicks reports whether an Update Advancements marking every
// criterion of id obtained arrives within n ticks.
func doneWithinTicks(t *testing.T, srv *server.Server, c *servertest.Client, n int, id string) bool {
	t.Helper()
	return c.ReadTicks(srv, n, func(packet int32, data []byte) bool {
		if packet != protocol.IDPlayUpdateAdvancements {
			return false
		}
		criteria := parseUpdate(t, data).progress[id]
		for _, at := range criteria {
			if at == nil {
				return false
			}
		}
		return len(criteria) > 0
	})
}

// TestSoundOfMusicIsGrantedForADiscInAMeadowJukebox: a disc put into a
// jukebox is an item_used_on_block that passes only where the jukebox's
// cell is meadow. vanilla: ItemUsedOnLocationTrigger, LocationPredicate,
// JukeboxPlayablePredicate
func TestSoundOfMusicIsGrantedForADiscInAMeadowJukebox(t *testing.T) {
	t.Parallel()
	const meadows = "minecraft:adventure/play_jukebox_in_meadows"
	srv, c := servertest.AttachActor(t)
	c.WatchAdvancements = true
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:jukebox")
	servertest.Setblock(t, c, 3, 63, 5, "minecraft:jukebox")
	// 3 63 5 is in the cell x 0..3, y 60..63, z 4..7, apart from 3 63 0's.
	c.ChatCommand("fillbiome 3 63 5 3 63 5 minecraft:meadow")
	servertest.UntilTicks(t, srv, 40, "the jukebox's cell is meadow", func() bool {
		return srv.OverworldBiomeAt(3, 63, 5) == "minecraft:meadow"
	})
	if got := srv.OverworldBiomeAt(3, 63, 0); got == "minecraft:meadow" {
		t.Fatalf("the first jukebox's cell is %s too", got)
	}

	servertest.PlaceFrom(c, "minecraft:music_disc_cat", 0, 0, -90, 3, 63, 0, 1)
	if doneWithinTicks(t, srv, c, 5, meadows) {
		t.Fatal("a disc in a jukebox outside a meadow granted play_jukebox_in_meadows")
	}
	servertest.PlaceFrom(c, "minecraft:music_disc_cat", 3, 3, 0, 3, 63, 5, 1)
	if !doneWithinTicks(t, srv, c, 5, meadows) {
		t.Error("a disc in a meadow jukebox did not grant play_jukebox_in_meadows")
	}
}
