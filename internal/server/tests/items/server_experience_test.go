package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The experience tests: the orbs a kill or a broken ore leaves, the bar they
// fill, the commands that move it, and what survives a death or a rejoin.
// They drive a server through real connections the way the survival and mob
// tests do, and they read the answer off the Set Experience packet, which is
// what a vanilla client builds its bar from.

// readExperience reads packets until a Set Experience arrives, answering
// keep-alives and stepping over the background an orb or a command leaves
// behind: the orbs' own packets, the movements, the chunks, the feedback.
func readExperience(t *testing.T, c *servertest.Client) servertest.ExperienceReport {
	t.Helper()
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		t.Helper()
		switch gotID {
		case protocol.IDPlaySetExperience:
			r := protocol.NewReader(data)
			report := servertest.ExperienceReport{Progress: r.ReadF32(), Level: r.ReadVarInt(), Total: r.ReadVarInt()}
			if err := r.Err(); err != nil {
				t.Fatalf("set experience: %v", err)
			}
			if r.Remaining() != 0 {
				t.Errorf("set experience has %d bytes left over", r.Remaining())
			}
			return report
		case protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayPickupItem, protocol.IDPlayRemoveEntities,
			protocol.IDPlaySystemChat, protocol.IDPlaySoundEffect,
			protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayWorldEvent,
			protocol.IDPlaySetEntityMotion,
			protocol.IDPlayAckBlockChange, protocol.IDPlayPingResponse,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk:
			// Background: the orb's own packets among them.
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for the experience", gotID)
		}
	}
	t.Fatal("no Set Experience packet arrived")
	return servertest.ExperienceReport{}
}

// awaitDeath reads packets until the Combat Death packet the dying player's
// own connection is sent, stepping over everything else a death, a taken orb
// or the world around them puts on the wire first.
func awaitDeath(t *testing.T, c *servertest.Client) {
	t.Helper()
	for gotID := range c.Packets(servertest.ExpectTimeout) {
		t.Helper()
		switch gotID {
		case protocol.IDPlayCombatDeath:
			return
		case protocol.IDPlaySetHealth, protocol.IDPlayDamageEvent,
			protocol.IDPlayHurtAnimation, protocol.IDPlayEntityEvent,
			protocol.IDPlaySystemChat, protocol.IDPlaySoundEffect,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayPickupItem, protocol.IDPlayRemoveEntities,
			protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayWorldEvent,
			protocol.IDPlaySetEntityMotion,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk:
			// Background: the death's own telling among it.
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for the death", gotID)
		}
	}
	t.Fatal("no Combat Death packet arrived")
}

// TestAKillingACowEndsInOrbsAndExperience checks the whole path of a kill's
// experience: the cow dies to the player's sword, the orb its death leaves is
// worth the one to three Animal.getBaseExperienceReward rolls, the Take Item
// Entity packet plays it flying to the killer who stood beside it, and the
// Set Experience that follows says what the worth did to the bar.
func TestAKillingACowEndsInOrbsAndExperience(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, entityID := servertest.JoinAs(t, cfg, "Butcher")

	// One blow is the cow's ten health: the fist's one, the netherite
	// sword's seven and sharpness III's two. Between two blows the first's
	// panic would run the cow out to the edge of the reach, and the orb
	// dropped there creeps back over the ground for hundreds of ticks.
	srv.GiveSlot("Butcher", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 3))

	// The cow stands a block beside the player, so the orb its death leaves
	// is inside the grown body the pickup sweeps with from the tick it
	// first moves.
	srv.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	servertest.AwaitFullSwing(t, srv, "Butcher")
	c.Attack(cow.EntityID)

	// Two orbs of one may merge into one before the pickup, so the wait is
	// for the bar to hold the orbs' whole worth rather than for a pickup per
	// orb. The wait also wants a
	// pickup recorded: the Set Experience of a take is written ahead of the
	// Take Item Entity packet announcing it, so a single-orb award's bar
	// matches its worth one packet ahead of the pickup, and a stop on the
	// bar alone would come back before the pickup was read.
	w := servertest.NewOrbWatcher(t, c)
	w.ReadUntilTicks(srv, 200, func() bool {
		if len(w.OrbIDs) == 0 || len(w.Exps) == 0 || len(w.Takes) == 0 {
			return false
		}
		worth := 0
		for _, v := range w.OrbIDs {
			worth += v
		}
		// The pickup's Take Item can trail the bar's update, so both are
		// waited for.
		return int(w.Exps[len(w.Exps)-1].Total) == worth && len(w.Takes) > 0
	})
	// The kill's experience arrives as the orbs the award splits it into:
	// a roll of two is two orbs of one, the getExperienceValue ladder's
	// own cut, and a roll of three is one orb of three.
	value := 0
	for _, v := range w.OrbValues() {
		if v < 1 || v > 3 {
			t.Errorf("the cow left an orb worth %d, want a piece of the Animal roll of one to three", v)
		}
		value += v
	}
	if value < 1 || value > 3 {
		t.Errorf("the cow's orbs are worth %d together, want the Animal roll of one to three", value)
	}
	if len(w.Takes) == 0 || len(w.Takes) > len(w.OrbIDs) {
		t.Fatalf("the orbs were taken %d times, want one to %d (merged orbs are taken once)", len(w.Takes), len(w.OrbIDs))
	}
	for _, got := range w.Takes {
		if got.Collector != entityID || got.Count != 1 {
			t.Errorf("take = orb %d by %d x%d, want the orb by the killer x1",
				got.Orb, got.Collector, got.Count)
		}
	}
	report := w.LastExperience()
	if report.Total != int32(value) {
		t.Errorf("experience total = %d, want the orbs' %d", report.Total, value)
	}
	if report.Level != 0 {
		t.Errorf("experience level = %d, want 0 (a value under the seven a level takes)", report.Level)
	}
	if math.Abs(float64(report.Progress)-float64(value)/7) > 0.02 {
		t.Errorf("experience progress = %g, want the orbs' share of the seven a level takes", report.Progress)
	}
}

// TestAnOrbFollowsAPlayerWithinEightBlocks checks the follow: an orb
// summoned five blocks from the player is pulled toward them, its movement
// packets saying so, until it is close enough to be taken.
func TestAnOrbFollowsAPlayerWithinEightBlocks(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Caller")

	srv, c, _ := servertest.JoinAs(t, cfg, "Caller", setup)

	c.ChatCommand("summon minecraft:experience_orb 5.5 64 0.5")

	// The orb the summon stood up, by the spawn packet that put it in the
	// world.
	w := servertest.NewOrbWatcher(t, c)
	w.ReadUntilTicks(srv, 60, func() bool { return len(w.OrbIDs) > 0 })
	if len(w.OrbIDs) != 1 {
		t.Fatalf("the summon left %d orbs, want the one it stood up", len(w.OrbIDs))
	}
	var orbID int32
	for id := range w.OrbIDs {
		orbID = id
	}

	// The follow: the orb's movement packets, read until one carries it
	// toward the player whose feet stand five blocks west of where it was
	// summoned.
	moved := c.ReadTicksSkipping(srv, 200, nil, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityPosition:
			return r.ReadVarInt() == orbID && r.ReadVarInt() >= 0 && r.ReadI16() < 0
		case protocol.IDPlayTeleportEntity:
			return r.ReadVarInt() == orbID && r.ReadVarInt() == 0 && r.ReadF64() < 5.4
		}
		return false
	})
	if !moved {
		t.Error("the orb never moved toward the player standing five blocks away")
	}
}

// TestAnOrbUnderTheWorldIsGone checks the below-world check of vanilla's
// base tick, Entity.checkBelowWorld: one whose body stands beneath the
// dimension's bottom and its sixty-four more is discarded the first tick,
// the removal the client is told of right after the spawn that showed it.
func TestAnOrbUnderTheWorldIsGone(t *testing.T) {
	t.Parallel()
	// The orb the test watches is stood up by a /summon, whose runner is
	// listed as an operator.
	cfg, setup := servertest.ExperienceSetup(t, "Caller")

	srv, c, _ := servertest.JoinAs(t, cfg, "Caller", setup)

	c.ChatCommand("summon minecraft:experience_orb 8.5 -200 16.5")

	// The orb the summon stood up is shown where it stands, two hundred
	// blocks beneath the world, and is gone by its first tick.
	w := servertest.NewOrbWatcher(t, c)
	w.ReadUntilTicks(srv, 60, func() bool { return len(w.OrbIDs) > 0 })
	if len(w.OrbIDs) != 1 {
		t.Fatalf("the summon left %d orbs, want the one it stood up", len(w.OrbIDs))
	}
	var orbID int32
	for id := range w.OrbIDs {
		orbID = id
	}
	removed := c.ReadTicksSkipping(srv, 60, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		for n := r.ReadVarInt(); n > 0 && r.Err() == nil; n-- {
			if r.ReadVarInt() == orbID {
				return true
			}
		}
		return false
	})
	if !removed {
		t.Error("the orb two hundred blocks under the world was never taken away")
	}
}

// TestMiningDiamondOreDropsExperience checks the ore half of the sources: a
// diamond ore broken for its drops by a survival pickaxe leaves the three to
// seven of its DropExperienceBlock range behind as orbs, which the player who
// broke it then walks over.
func TestMiningDiamondOreDropsExperience(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Miner")

	// An iron pickaxe, which is what a diamond ore holds out for.
	pick, ok := registry.ItemID("minecraft:iron_pickaxe")
	if !ok {
		t.Fatal("the item registry has no iron pickaxe")
	}
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: pick, Count: 1})

	// The miner steps up to the ore before breaking it: one block diagonal
	// of it, the way a player stands at the block they mine. The distance
	// matters — an orb whose pop left it resting on the floor of the hole
	// the ore left is only lifted out by the follow when the player it
	// pulls toward stands close, and only a risen orb is one a player can
	// reach.
	c.MoveToGround(1.5, 64, 1.5, true)

	// The ore, two blocks east, standing in for the stone the flat world
	// grew there.
	ore, ok := registry.FindState("minecraft:diamond_ore", nil)
	if !ok {
		t.Fatal("the block registry has no diamond ore")
	}
	if !srv.SetBlock(2, 63, 2, ore) {
		t.Fatal("the ore's chunk was not loaded")
	}
	c.Settle()

	c.BreakBlock(2, 63, 2)

	w := servertest.NewOrbWatcher(t, c)
	w.ReadUntilTicks(srv, 200, func() bool {
		return len(w.OrbIDs) > 0 &&
			len(w.Takes) >= len(w.OrbIDs) && len(w.Exps) >= len(w.OrbIDs)
	})
	values := w.OrbValues()
	if len(values) == 0 {
		t.Fatal("the broken ore left no orbs")
	}
	// The ore's range is what the orbs together are worth: the three to
	// seven of its DropExperienceBlock, cut into the pieces of the ladder
	// the award splits by - a roll of four is a three and a one.
	total := 0
	for _, v := range values {
		total += v
	}
	if total < 3 || total > 7 {
		t.Errorf("the ore's orbs are worth %d together, want the DropExperienceBlock range of three to seven", total)
	}
	report := w.LastExperience()
	if report.Total < 3 || report.Total > 7 {
		t.Errorf("experience total = %d, want the ore's three to seven", report.Total)
	}
	// The level the total lands on: one only where the roll filled the
	// seven a first level takes, never past it.
	if want := report.Total / 7; report.Level != want {
		t.Errorf("experience level = %d, want %d", report.Level, want)
	}
}

// TestXpCommandsGiveVanillasNumbers checks the command half: an add of points
// walks the bar through the levels the points are worth, an add of levels
// adds them, a set puts one or the other outright, a query reads either back,
// and each answers with the translation key vanilla's own ExperienceCommand
// sends.
func TestXpCommandsGiveVanillasNumbers(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Charter")

	_, c, _ := servertest.JoinAs(t, cfg, "Charter", setup)

	// Thirty points on an empty bar: the crossings the walk takes grow the
	// level's own need as they go, so thirty points land three levels up
	// with three of the thirteen the fourth needs - Player.
	// giveExperiencePoints' walk of the crossings, curve and all.
	c.ChatCommand("xp add @s 30 points")
	report := readExperience(t, c)
	thirty := int64(30)
	c.ExpectCommandFeedbackText("commands.experience.add.points.success.single",
		protocol.Text{Number: &thirty}, servertest.PlayerDisplayComponent("Charter"))
	if report.Total != 30 || report.Level != 3 {
		t.Errorf("after 30 points: total=%d level=%d, want 30 and 3", report.Total, report.Level)
	}
	if math.Abs(float64(report.Progress)-3.0/13.0) > 0.02 {
		t.Errorf("after 30 points: progress = %g, want the three points left of the crossing", report.Progress)
	}

	// Two levels on top: the fifth, with the bar's fill and the total
	// riding through untouched, the way giveExperienceLevels leaves them.
	c.ChatCommand("xp add @s 2 levels")
	report = readExperience(t, c)
	twoLevels := int64(2)
	c.ExpectCommandFeedbackText("commands.experience.add.levels.success.single",
		protocol.Text{Number: &twoLevels}, servertest.PlayerDisplayComponent("Charter"))
	if report.Total != 30 || report.Level != 5 || math.Abs(float64(report.Progress)-3.0/13.0) > 0.02 {
		t.Errorf("after 2 levels: total=%d level=%d progress=%g, want 30, 5 and the fill unchanged",
			report.Total, report.Level, report.Progress)
	}

	// A set of levels puts the level outright and nothing else.
	c.ChatCommand("xp set @s 3 levels")
	report = readExperience(t, c)
	threeLevels := int64(3)
	c.ExpectCommandFeedbackText("commands.experience.set.levels.success.single",
		protocol.Text{Number: &threeLevels}, servertest.PlayerDisplayComponent("Charter"))
	if report.Total != 30 || report.Level != 3 {
		t.Errorf("after set 3 levels: total=%d level=%d, want 30 and 3", report.Total, report.Level)
	}

	// The queries read the two halves back: the level, and the points the
	// progress into the level stands at - which is two, the float chain the
	// crossings ran in landing a hair under the three the thirds sum to,
	// the same hair vanilla's own float arithmetic lands on.
	c.ChatCommand("xp query @s levels")
	threeQueried := int64(3)
	c.ExpectCommandFeedbackText("commands.experience.query.levels",
		servertest.PlayerDisplayComponent("Charter"), protocol.Text{Number: &threeQueried})
	c.ChatCommand("xp query @s points")
	twoQueried := int64(2)
	c.ExpectCommandFeedbackText("commands.experience.query.points",
		servertest.PlayerDisplayComponent("Charter"), protocol.Text{Number: &twoQueried})

	// A set of points puts the fill within the level, and one naming a
	// whole level's need or more is refused with the vanilla error.
	c.ChatCommand("xp set @s 5 points")
	report = readExperience(t, c)
	fivePoints := int64(5)
	c.ExpectCommandFeedbackText("commands.experience.set.points.success.single",
		protocol.Text{Number: &fivePoints}, servertest.PlayerDisplayComponent("Charter"))
	if report.Level != 3 || report.Total != 30 {
		t.Errorf("after set 5 points: level=%d total=%d, want 3 and 30", report.Level, report.Total)
	}
	if math.Abs(float64(report.Progress)-5.0/13.0) > 0.02 {
		t.Errorf("after set 5 points: progress = %g, want five thirteenths", report.Progress)
	}
	c.ChatCommand("xp set @s 13 points")
	servertest.ExpectCommandError(t, c, "commands.experience.set.points.invalid")

	c.Disconnect()
}

// TestXpSetPointsSkipsOnlyTheRefusingTargets checks each target is tested on
// its own: the player whose level needs more than the amount is set, the one
// it would overfill is skipped, and the feedback names the one that was set.
func TestXpSetPointsSkipsOnlyTheRefusingTargets(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Alice")
	_, connect := servertest.Start(t, cfg, setup)
	alice := connect()
	alice.JoinAs("Alice")
	bob := connect()
	bob.JoinAs("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("xp set @s 30 levels")
	readExperience(t, alice)
	alice.Drain()

	// Bob, the second target, is refused; Alice, the first, is still set.
	alice.ChatCommand("xp set @a 50 points")
	report := readExperience(t, alice)
	if report.Level != 30 {
		t.Errorf("Alice after set 50 points: level=%d, want 30", report.Level)
	}
	servertest.AwaitCommandFeedbackText(t, alice, "commands.experience.set.points.success.single",
		servertest.NumberText(50), servertest.PlayerDisplayComponent("Alice"))

	// Nobody can take 50 points once Alice is back at level 0.
	alice.ChatCommand("xp set @s 0 levels")
	readExperience(t, alice)
	alice.Drain()
	alice.ChatCommand("xp set @a 50 points")
	servertest.ExpectCommandError(t, alice, "commands.experience.set.points.invalid")
}

// TestXpSelectorWithSpacesInItsBrackets checks the targets argument read
// whole: the amount and the kind sit behind the selector, so an add lands for
// the named player and a query answers, rather than the amount being taken
// from one of the selector's own options.
func TestXpSelectorWithSpacesInItsBrackets(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Charter")

	_, c, _ := servertest.JoinAs(t, cfg, "Charter", setup)

	c.ChatCommand("xp add @s[ name=Charter ] 5")
	if report := readExperience(t, c); report.Total != 5 {
		t.Errorf("total after xp add = %d, want 5", report.Total)
	}
	five := int64(5)
	c.ExpectCommandFeedbackText("commands.experience.add.points.success.single",
		protocol.Text{Number: &five}, servertest.PlayerDisplayComponent("Charter"))

	c.ChatCommand("xp query @s[ name=Charter ] points")
	c.ExpectCommandFeedbackText("commands.experience.query.points",
		servertest.PlayerDisplayComponent("Charter"), protocol.Text{Number: &five})

	c.Disconnect()
}

// enterWorldReadingExperience runs the join sequence the way enterWorldAs
// does, reading on the way the Set Experience packet the join carries — the
// one the vanilla server sends between the held item and the recipes, which
// is what the player's file was read into.
func enterWorldReadingExperience(t *testing.T, c *servertest.Client, name string) servertest.ExperienceReport {
	t.Helper()
	var entityID int32
	var found bool
	var report servertest.ExperienceReport
	for packetID, data := range c.Packets(servertest.JoinTimeout) {
		t.Helper()
		switch {
		case packetID == protocol.IDPlayLogin && entityID == 0:
			entityID = protocol.NewReader(data).ReadI32()
		case packetID == protocol.IDPlaySetExperience:
			r := protocol.NewReader(data)
			report = servertest.ExperienceReport{Progress: r.ReadF32(), Level: r.ReadVarInt(), Total: r.ReadVarInt()}
			if err := r.Err(); err != nil {
				t.Fatalf("set experience: %v", err)
			}
			if r.Remaining() != 0 {
				t.Errorf("set experience has %d bytes left over", r.Remaining())
			}
			found = true
		case packetID == protocol.IDPlayChunkBatchFinished:
			if entityID == 0 {
				t.Fatal("login (play) never arrived")
			}
			if !found {
				t.Fatal("the join carried no Set Experience packet")
			}
			c.MarkLoaded()
			return report
		}
	}
	t.Fatalf("the join did not finish within %s", servertest.JoinTimeout)
	return servertest.ExperienceReport{}
}

// TestExperienceSurvivesARejoin checks the persistence: the levels a player
// banked are the levels the join's own Set Experience packet tells their
// client about when they come back, read from the file under vanilla's
// XpLevel.
func TestExperienceSurvivesARejoin(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Returnee")

	srv, connect := servertest.Start(t, cfg, setup)
	c := connect()
	c.JoinAs("Returnee")

	c.ChatCommand("xp add @s 2 levels")
	readExperience(t, c)
	c.Disconnect()

	// A whole new connection, the same player: the file the leaving wrote
	// is the one the join reads.
	srv.FlushSaves()
	again := connect()
	again.Login("Returnee")
	again.Configure()
	report := enterWorldReadingExperience(t, again, "Returnee")
	if report.Level != 2 || report.Total != 0 || report.Progress != 0 {
		t.Errorf("the rejoin read level=%d total=%d progress=%g, want 2, 0 and 0",
			report.Level, report.Total, report.Progress)
	}
	again.Disconnect()
}

// TestADeathDropsItsExperienceAtTheFeet checks where a death's orbs are
// awarded: the body's own position, the feet, not half a block above them.
// Two levels are fourteen points, and every orb the award splits them into
// is laid where the body fell.
func TestADeathDropsItsExperienceAtTheFeet(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Dier")
	srv.SetExperienceLevels("Dier", 2)

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	orb := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	if orb.X != 0.5 || orb.Y != 64 || orb.Z != 0.5 {
		t.Errorf("the death's orb spawned at (%v, %v, %v), want the body's feet (0.5, 64, 0.5)",
			orb.X, orb.Y, orb.Z)
	}
}

// TestExperienceSurvivesARejoinOnTheDeathScreenUnderKeepInventory checks the
// file a player leaves on the death screen: with keep_inventory on, nothing
// was dropped to the ground, so the rejoin's own Set Experience carries the
// bar back rather than starting empty.
func TestExperienceSurvivesARejoinOnTheDeathScreenUnderKeepInventory(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Quitter")

	srv, connect := servertest.Start(t, cfg, setup)
	srv.SetGameRule(world.RuleKeepInventory.String(), true)

	c := connect()
	c.JoinAs("Quitter")

	// Two levels and three points, so the level, the running total and the
	// bar's fill all stand above zero and a death that reset either of the
	// last two would show.
	c.ChatCommand("xp add @s 2 levels")
	readExperience(t, c)
	c.ChatCommand("xp add @s 3 points")
	before := readExperience(t, c)

	c.ChatCommand("kill @s")
	awaitDeath(t, c)
	c.Disconnect()

	// A whole new connection, the same player: the file the dead leaving
	// wrote is the one the join reads.
	srv.FlushSaves()
	again := connect()
	again.Login("Quitter")
	again.Configure()
	report := enterWorldReadingExperience(t, again, "Quitter")
	if report != before {
		t.Errorf("the rejoin read level=%d total=%d progress=%g, want the kept level=%d total=%d progress=%g",
			report.Level, report.Total, report.Progress, before.Level, before.Total, before.Progress)
	}
	again.Disconnect()
}

// TestSpectatorExperienceSurvivesARejoinOnTheDeathScreen checks the other
// case a death drops nothing for: /kill reaches a spectator, and the bar its
// file carries is read back by the rejoin's Set Experience.
func TestSpectatorExperienceSurvivesARejoinOnTheDeathScreen(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Ghost")
	cfg.GameMode = "spectator"

	srv, connect := servertest.Start(t, cfg, setup)
	c := connect()
	c.JoinAs("Ghost")

	srv.SetExperienceLevels("Ghost", 12)
	c.Settle()
	c.ChatCommand("kill @s")
	awaitDeath(t, c)
	c.Disconnect()

	srv.FlushSaves()
	again := connect()
	again.Login("Ghost")
	again.Configure()
	if report := enterWorldReadingExperience(t, again, "Ghost"); report.Level != 12 {
		t.Errorf("the rejoin read level=%d, want 12 kept by the spectator's death", report.Level)
	}
	again.Disconnect()
}

// TestExperienceIsNotKeptOnARejoinWithoutKeepInventory checks the other side
// of the death screen: a death that dropped its experience leaves none for
// the rejoin, whose bar starts empty.
func TestExperienceIsNotKeptOnARejoinWithoutKeepInventory(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Quitter")

	srv, connect := servertest.Start(t, cfg, setup)
	c := connect()
	c.JoinAs("Quitter")

	c.ChatCommand("xp add @s 2 levels")
	readExperience(t, c)

	// Die away from the world spawn the rejoin lands at, so the orbs the
	// death leaves cannot be swept back up on the way in and hide an empty
	// bar behind a pickup.
	c.ChatCommand("tp @s 200 64 200")
	c.Settle()
	c.ChatCommand("kill @s")
	awaitDeath(t, c)
	c.Disconnect()

	srv.FlushSaves()
	again := connect()
	again.Login("Quitter")
	again.Configure()
	report := enterWorldReadingExperience(t, again, "Quitter")
	if report.Level != 0 || report.Total != 0 || report.Progress != 0 {
		t.Errorf("the rejoin read level=%d total=%d progress=%g, want the empty bar a dropped death leaves",
			report.Level, report.Total, report.Progress)
	}
	again.Disconnect()
}

// TestADeathDropsItsExperienceAndRespawnsWithoutIt checks both ends of a
// death: the min(level*7, 100) of Player.getBaseExperienceReward left as orbs
// where the body fell, a player standing there taking them, and the bar the
// respawn sends coming back empty.
func TestADeathDropsItsExperienceAndRespawnsWithoutIt(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Taker", "Dier")

	srv, connect := servertest.Start(t, cfg, setup)
	taker := connect()
	taker.JoinAs("Taker")

	dier := connect()
	dier.JoinAs("Dier")
	dier.MoveTo(1.5, 64, 0.5, 0, 0)
	taker.Settle()
	dier.Settle()

	// Two levels on the one who is about to die, which is fourteen points
	// of death drop - the seven a level the reward multiplies, cut into the
	// two sevens the award splits it into. The set's own bar packet goes to
	// the one it was set on.
	taker.ChatCommand("xp set Dier 2 levels")
	taker.Settle()
	dier.Settle()

	taker.ChatCommand("kill Dier")

	// The taker stands a block east of where the body fell, so both orbs
	// the death left are inside the sweep the pickup runs with, and the
	// two-tick wait between them is the only thing spacing the takes.
	w := servertest.NewOrbWatcher(t, taker)
	w.ReadUntilTicks(srv, 240, func() bool {
		return len(w.Takes) >= 2 && len(w.Exps) >= 2
	})
	// Two sevens, as two orbs - or, one award in forty, as one: the
	// second seven's ExperienceOrb.tryMergeToExisting rolls nextInt(40)
	// against the first orb's entity ID and, when they line up, counts
	// itself into that orb instead, which is then taken twice.
	values := w.OrbValues()
	if len(values) != 1 && len(values) != 2 {
		t.Fatalf("the death left %d orbs, want the two sevens the award split fourteen into", len(values))
	}
	for _, v := range values {
		if v != 7 {
			t.Errorf("the death left an orb worth %d, want the seven of the split", v)
		}
	}
	if len(w.Takes) < 2 {
		t.Fatalf("the orbs were taken %d times, want both sevens", len(w.Takes))
	}
	report := w.LastExperience()
	// Fourteen points from nothing: the first seven fills the level it
	// takes and the second is seven-ninths of the wider one that follows,
	// the curve the crossings walk.
	if report.Total != 14 || report.Level != 1 || math.Abs(float64(report.Progress)-7.0/9.0) > 0.02 {
		t.Errorf("after both orbs: total=%d level=%d progress=%g, want 14, 1 and seven-ninths",
			report.Total, report.Level, report.Progress)
	}

	// The dead one respawns into a bar the death emptied.
	awaitDeath(t, dier)
	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	awaitRespawn(t, dier)
	dier.ExpectHealth()
	respawned := readExperience(t, dier)
	if respawned.Total != 0 || respawned.Level != 0 || respawned.Progress != 0 {
		t.Errorf("the respawn read total=%d level=%d progress=%g, want the empty bar",
			respawned.Total, respawned.Level, respawned.Progress)
	}
	dier.Disconnect()
	taker.Disconnect()
}

// awaitRespawn reads packets until the Respawn packet the respawn sequence
// opens with, stepping over everything else the dying minute put on the wire.
func awaitRespawn(t *testing.T, c *servertest.Client) {
	t.Helper()
	for gotID := range c.Packets(servertest.ExpectTimeout) {
		t.Helper()
		switch gotID {
		case protocol.IDPlayRespawn:
			return
		case protocol.IDPlayEntityEvent, protocol.IDPlaySystemChat, protocol.IDPlaySoundEffect,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayPickupItem, protocol.IDPlayRemoveEntities,
			protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayWorldEvent,
			protocol.IDPlaySetEntityMotion,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk:
			// Background: the world going on without the body.
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for the respawn", gotID)
		}
	}
	t.Fatal("no Respawn packet arrived")
}

// TestTwoOrbsNextToEachOtherMerge checks the merging: two orbs of one value
// whose ids differ by the forty the merge rule asks for, lying within half a
// block of each other, become one - the absorbed one's pickups joined into
// the absorber's count and its body taken out of the world.
func TestTwoOrbsNextToEachOtherMerge(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Watcher")

	// The pair the rule asks for, with the thirty-nine orbs between their
	// ids standing far off where no merge of theirs can interfere: the rule
	// is ids forty apart, and the ids run in spawn order. The watcher reads
	// from before the first spawn, so its record of the orbs is complete.
	w := servertest.NewOrbWatcher(t, c)
	first := srv.SpawnOrb(8, 64, 8, 1)
	for range 39 {
		srv.SpawnOrb(300, 64, 300, 1)
	}
	second := srv.SpawnOrb(8.2, 64, 8, 1)

	// One of the pair is gone within the scan's rhythm: the twentieth tick
	// of whichever orb scans first, or a second later if its first scan ran
	// before its neighbour existed.
	removed := int32(0)
	w.ReadUntilTicks(srv, 120, func() bool { return len(w.Removed) > 0 })
	for _, id := range w.Removed {
		if id == first || id == second {
			removed = id
		}
	}
	if removed == 0 {
		t.Logf("removed=%v orbIDs=%v takes=%v", w.Removed, w.OrbIDs, w.Takes)
		t.Fatal("neither of the pair was merged away within the scan's rhythm")
	}

	// The survivor still lies where the pair was, worth what it was worth,
	// and a player standing near it takes the whole of the merged count:
	// two pickups, two ticks apart, of the one orb that is left.
	c.MoveTo(8.5, 64, 8.5, 0, 0)
	w.ReadUntilTicks(srv, 240, func() bool {
		return len(w.Takes) >= 2 && len(w.Exps) >= 2
	})
	if len(w.Takes) < 2 {
		t.Fatalf("the merged orb was taken %d times, want the two pickups of the merged count", len(w.Takes))
	}
	report := w.LastExperience()
	if report.Total != 2 {
		t.Errorf("experience total after both pickups = %d, want the merged pair's two", report.Total)
	}
}

// TestAnAwardMergeReachesOnlyItsBox checks the reach of the award-time merge
// against vanilla ExperienceOrb.tryMergeToExisting: the unit box it queries
// about the award point against the orb's half-block body, so the orb must lie
// within 0.75 on the flat and its feet in (y-1, y+0.5). A fresh forty-first
// orb is the no-merge answer, an unchanged cluster the merge one.
func TestAnAwardMergeReachesOnlyItsBox(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		offset [3]float64
		want   int
	}{
		{"0.9 east is past the flat reach", [3]float64{0.9, 0, 0}, 41},
		{"0.9 above is past the top reach", [3]float64{0, 0.9, 0}, 41},
		{"a foot at y-1 is off the bottom", [3]float64{0, -1, 0}, 41},
		{"0.6 east is within the flat reach", [3]float64{0.6, 0, 0}, 40},
		{"0.4 above is within the top reach", [3]float64{0, 0.4, 0}, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := servertest.ExperienceSetup(t)
			srv, _, _ := servertest.JoinAs(t, cfg, "Watcher")
			srv.StopTick()

			// Forty orbs of the award's value at the offset, their ids the
			// forty residues mod 40, so whichever residue the award's
			// one-in-forty shift names, one of them is a merge candidate and
			// the reach alone decides whether it takes the award.
			for range 40 {
				srv.SpawnOrb(8+tc.offset[0], 64+tc.offset[1], 8+tc.offset[2], 1)
			}
			if got := srv.OrbCount(); got != 40 {
				t.Fatalf("the cluster stands %d orbs, want the forty laid out", got)
			}
			srv.AwardExperienceAt(8, 64, 8, 1)
			if got := srv.OrbCount(); got != tc.want {
				t.Fatalf("after the award %d orbs stand, want %d", got, tc.want)
			}
		})
	}
}

// TestAnOrbStuckInABlockIsPushedOut lays an orb inside a stone cell and
// drives sixty entity ticks by hand: the vanilla tick answers a body with no
// follower whose next move would still cross a block with the
// moveTowardsClosestSpace push, so the orb ends the sixty ticks outside the
// block.
func TestAnOrbStuckInABlockIsPushedOut(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, c := servertest.Join(t, cfg, "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SetFarmBlock(t, c, srv, 5, 80, 0, "minecraft:stone", nil)
	id := srv.SpawnOrb(5.5, 80.2, 0.5, 1)
	srv.TickEntities(60)

	x, y, z, ok := srv.OrbPos(id)
	if !ok {
		t.Fatal("the orb left the world")
	}
	if x >= 5 && x < 6 && y >= 80 && y < 81 && z >= 0 && z < 1 {
		t.Errorf("the orb is still inside the stone after sixty ticks, at (%v, %v, %v)", x, y, z)
	}
}

// TestAnOrbRaisesThePlayersScore checks that a 7-point orb is told as score 7
// in the player's entity data, index 18, which the death screen reads.
func TestAnOrbRaisesThePlayersScore(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Scorer")

	score := int32(-1)
	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 7)
	c.ReadTicksSkipping(srv, 240, nil, func(id int32, data []byte) bool {
		w.See(id, data)
		if id == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			if r.ReadU8() == protocol.MetadataIndexScore && r.ReadVarInt() == protocol.MetadataVarInt {
				score = r.ReadVarInt()
				return true
			}
		}
		return false
	})
	if score != 7 {
		t.Errorf("score metadata = %d, want 7", score)
	}
}

// TestSilkTouchOreDropsNoExperience breaks a diamond ore with a Silk Touch
// pickaxe: the ore itself drops and no orb follows.
// vanilla: EnchantmentHelper.processBlockExperience
func TestSilkTouchOreDropsNoExperience(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Miner")
	srv.GiveSlot("Miner", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:iron_pickaxe", "minecraft:silk_touch", 1))
	c.MoveToGround(1.5, 64, 1.5, true)
	ore, ok := registry.FindState("minecraft:diamond_ore", nil)
	if !ok || !srv.SetBlock(2, 63, 2, ore) {
		t.Fatal("the ore could not be placed")
	}
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.BreakBlock(2, 63, 2)
	c.Settle()
	servertest.UntilTicks(t, srv, 100, "the ore to drop", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:diamond_ore" {
				return true
			}
		}
		return false
	})
	if n := srv.OrbCount(); n != 0 {
		t.Errorf("a silk-touched ore left %d orbs, want none", n)
	}
}

// TestOrbsOnAPlayerAreTakenOnePerTwoTicks puts three unmerged orbs on a
// survival player: the delay the first touch sets refuses the others, so one
// is taken now and the next every second tick afterwards.
// vanilla: ExperienceOrb.playerTouch
func TestOrbsOnAPlayerAreTakenOnePerTwoTicks(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Collector")
	servertest.HoldTicks(t, srv)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 1)
	srv.SpawnOrb(0.5, 64, 0.5, 3)
	srv.SpawnOrb(0.5, 64, 0.5, 7)

	taken := func(step string, ticks int, wantTakes int) {
		t.Helper()
		c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
			w.See(id, data)
			return false
		})
		if len(w.Takes) != wantTakes {
			t.Fatalf("%s: %d orbs taken, want %d (takes %v)", step, len(w.Takes), wantTakes, w.Takes)
		}
	}
	taken("after the first tick", 1, 1)
	taken("after two more ticks", 2, 2)
	taken("after two more ticks", 2, 3)
}

// TestSculkAndSpawnerBlocksDropExperience breaks each block with the given
// tool and sums the orbs it leaves; the loop is held so no orb is picked up
// before the sum.
// vanilla: SpawnerBlock.spawnAfterBreak, SculkCatalystBlock, SculkBlock
func TestSculkAndSpawnerBlocksDropExperience(t *testing.T) {
	t.Parallel()
	hoe := servertest.EnchantedStack(t, "minecraft:iron_hoe", "minecraft:silk_touch", 1)
	cases := []struct {
		name, block, tool string
		silk              bool
		min, max          int
	}{
		{"catalyst", "minecraft:sculk_catalyst", "minecraft:iron_hoe", false, 5, 5},
		{"sculk", "minecraft:sculk", "minecraft:iron_hoe", false, 1, 1},
		{"spawner", "minecraft:spawner", "minecraft:iron_pickaxe", false, 15, 43},
		{"catalyst silk touch", "minecraft:sculk_catalyst", "", true, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, _ := servertest.ExperienceSetup(t)
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c, _ := servertest.JoinAs(t, cfg, "Miner")
			if tc.silk {
				srv.GiveSlot("Miner", player.HotbarSlot, hoe)
			} else {
				id, _ := registry.ItemID(tc.tool)
				srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: int32(id), Count: 1})
			}
			c.MoveToGround(1.5, 64, 1.5, true)
			state, ok := registry.FindState(tc.block, nil)
			if !ok || !srv.SetBlock(5, 63, 1, state) {
				t.Fatal("the block could not be placed")
			}
			c.Settle()
			servertest.HoldTicks(t, srv)

			c.BreakBlock(5, 63, 1)
			c.Settle()
			// Hardness 3 to 5 takes a few ticks to mine out.
			servertest.UntilTicks(t, srv, 200, "the block to break", func() bool {
				return srv.BlockAt(5, 63, 1) != state
			})
			total := 0
			for _, o := range srv.OrbsInWorld() {
				total += o.XP
			}
			if total < tc.min || total > tc.max {
				t.Errorf("breaking %s left %d experience, want %d to %d", tc.block, total, tc.min, tc.max)
			}
		})
	}
}
