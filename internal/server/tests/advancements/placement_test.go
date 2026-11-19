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

// TestPlantingSeedsObtainsItsPlacedBlockCriterion: wheat seeds laid on
// farmland are a placed_block for the wheat crop, not for the other seeds.
// vanilla: BlockItem.place, ItemUsedOnLocationTrigger
func TestPlantingSeedsObtainsItsPlacedBlockCriterion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:farmland")
	servertest.PlaceFrom(c, "minecraft:wheat_seeds", 0, 0, -90, 3, 63, 0, 1)
	if !srv.AdvancementObtained("Actor", "minecraft:husbandry/plant_seed", "wheat") {
		t.Error("planting wheat seeds did not obtain plant_seed's wheat")
	}
	if srv.AdvancementObtained("Actor", "minecraft:husbandry/plant_seed", "beetroots") {
		t.Error("planting wheat seeds obtained plant_seed's beetroots")
	}
}

// TestUsingHoneycombOnCopperObtainsWaxOn: the click that waxes a copper block
// is an item_used_on_block; the same item on stone, which it does nothing to,
// is not. vanilla: ServerPlayerGameMode.useItemOn, ItemUsedOnLocationTrigger
func TestUsingHoneycombOnCopperObtainsWaxOn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:cobblestone")
	servertest.PlaceFrom(c, "minecraft:honeycomb", 0, 0, -90, 3, 63, 0, 1)
	if srv.AdvancementObtained("Actor", "minecraft:husbandry/wax_on", "wax_on") {
		t.Fatal("honeycomb on cobblestone obtained wax_on")
	}
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:copper_block")
	servertest.PlaceFrom(c, "minecraft:honeycomb", 0, 0, -90, 3, 63, 0, 1)
	if !srv.AdvancementObtained("Actor", "minecraft:husbandry/wax_on", "wax_on") {
		t.Error("waxing a copper block did not obtain wax_on")
	}
	if srv.AdvancementObtained("Actor", "minecraft:husbandry/wax_off", "wax_off") {
		t.Error("waxing obtained wax_off")
	}
}

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

// TestScrapingAWaxedBlockObtainsWaxOff: an axe on a waxed copper block is an
// item_used_on_block read before the wax comes off.
// vanilla: AxeItem.useOn, ItemUsedOnLocationTrigger
func TestScrapingAWaxedBlockObtainsWaxOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:waxed_copper_block")
	servertest.PlaceFrom(c, "minecraft:iron_axe", 0, 0, -90, 3, 63, 0, 1)
	if !srv.AdvancementObtained("Actor", "minecraft:husbandry/wax_off", "wax_off") {
		t.Error("scraping a waxed block did not obtain wax_off")
	}
	if srv.AdvancementObtained("Actor", "minecraft:husbandry/wax_on", "wax_on") {
		t.Error("scraping obtained wax_on")
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

// TestSleepingInABedObtainsSleepInBed: lying down in a bed is a slept_in_bed.
// vanilla: ServerPlayer.startSleepInBed, PlayerTrigger
func TestSleepingInABedObtainsSleepInBed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	if !srv.AdvancementObtained("Actor", "minecraft:adventure/sleep_in_bed", "slept_in_bed") {
		t.Error("lying down in a bed did not obtain sleep_in_bed")
	}
}

// TestShootingACrossbowObtainsOlBetsy: the shot of a loaded crossbow is a
// shot_crossbow for the crossbow item; charging it is not.
// vanilla: CrossbowItem.performShooting, ShotCrossbowTrigger
func TestShootingACrossbowObtainsOlBetsy(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:crossbow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 27)
	servertest.ReleaseBow(c)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
	if srv.AdvancementObtained("Archer", "minecraft:adventure/ol_betsy", "shot_crossbow") {
		t.Fatal("loading the crossbow obtained ol_betsy")
	}
	servertest.DrawBow(c, -90, 0)
	servertest.ExpectArrowSpawn(srv, c)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		return srv.AdvancementObtained("Archer", "minecraft:adventure/ol_betsy", "shot_crossbow")
	})
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

// TestLightAsARabbitIsGrantedOnPowderSnowInLeatherBoots: the once-a-second
// location trigger passes for a player on the ground on powder snow with
// leather boots in the feet slot, and not for one without them on obsidian.
// vanilla: ServerPlayer.tick, EquipmentPredicate, SteppingOnPredicate
func TestLightAsARabbitIsGrantedOnPowderSnowInLeatherBoots(t *testing.T) {
	t.Parallel()
	const rabbit = "minecraft:adventure/walk_on_powder_snow_with_leather_boots"
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 3, 63, 0, "minecraft:powder_snow")
	servertest.Setblock(t, c, 5, 63, 0, "minecraft:obsidian")
	c.WatchAdvancements = true

	c.MoveToGround(5.5, 64, 0.5, true)
	if doneWithinTicks(t, srv, c, 40, rabbit) {
		t.Fatal("standing on obsidian without boots granted walk_on_powder_snow_with_leather_boots")
	}
	c.ChatCommand("item replace entity @s armor.feet with minecraft:leather_boots")
	c.MoveToGround(3.5, 64, 0.5, true)
	if !doneWithinTicks(t, srv, c, 40, rabbit) {
		t.Error("standing on powder snow in leather boots did not grant walk_on_powder_snow_with_leather_boots")
	}
}
