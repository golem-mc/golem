package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// reinforcedZombie is one zombie standing after a call: where it is, whom
// it hunts, and whether it is the one the player struck.
type reinforcedZombie struct {
	id      int32
	x, y, z float64
	target  int32
	struck  bool
}

// reinforcementRun hits a zombie that is certain to call, in a world of the
// difficulty given, and returns the zombies standing after the ticks given
// together with the player's ID and the struck zombie's ID.
func reinforcementRun(t *testing.T, difficulty uint8, ticks uint64, rules ...string) (zombies []reinforcedZombie, playerID int32) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.SetSpawning(false)
	for _, rule := range rules {
		srv.SetGameRule(rule, false)
	}
	srv.SetDayTime(18000) // midnight: dark enough for the called zombie to stand
	srv.SetDifficulty(difficulty)
	c := connect()
	playerID = c.JoinAs("Attacker")

	servertest.HoldTicks(t, srv)
	struck := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
	srv.SetMobReinforcementBase(struck, 1)
	swing := func() {
		servertest.UntilTicks(t, srv, 40, "the swing recovers", func() bool { return srv.AttackTicks("Attacker") >= servertest.SwingRecovered })
	}
	swing()
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Attack(struck)
	c.Settle()

	// A call's site search is a handful of dice-driven tries that may all
	// miss, so a certain caller is struck again until one finds a site. The
	// loop is held so the zombies are read on the tick the call lands.
	for ran := uint64(0); ran < ticks && srv.MobCount() < 2; ran++ {
		servertest.RunTicks(t, srv, 1)
		if ran%20 == 19 && srv.MobCount() < 2 {
			swing()
			c.Attack(struck)
			c.Settle()
		}
	}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:zombie" {
			continue
		}
		zombies = append(zombies, reinforcedZombie{m.ID, m.X, m.Y, m.Z, srv.MobTargetID(m.ID), m.ID == struck})
	}
	return zombies, playerID
}

// TestAHurtZombieOnHardCallsAReinforcement checks Zombie.hurtServer: a zombie
// a player strikes on Hard, certain to call, brings a second zombie at least
// seven blocks off along some axis that hunts the same player.
func TestAHurtZombieOnHardCallsAReinforcement(t *testing.T) {
	t.Parallel()
	zombies, playerID := reinforcementRun(t, 3, 100)
	if len(zombies) != 2 {
		t.Fatalf("%d zombies stood after the struck one's call, want 2", len(zombies))
	}
	struck, called := zombies[0], zombies[1]
	if !struck.struck {
		struck, called = called, struck
	}
	if called.target != playerID {
		t.Errorf("the called zombie hunts %d, want the player %d", called.target, playerID)
	}
	// The site is 7 or more from the caller's block, not from its exact
	// position, so a caller standing late in its block can be under 7
	// blocks away by float.
	far := math.Max(math.Abs(math.Floor(called.x)-math.Floor(struck.x)),
		math.Max(math.Abs(math.Floor(called.y)-math.Floor(struck.y)), math.Abs(math.Floor(called.z)-math.Floor(struck.z))))
	if far < 7 {
		t.Errorf("the called zombie stands %.0f blocks from the caller's block on its farthest axis, want 7 or more", far)
	}
}

// TestAHurtZombieBelowHardCallsNoReinforcement checks the difficulty gate:
// Easy and Normal never call, whatever the chance.
func TestAHurtZombieBelowHardCallsNoReinforcement(t *testing.T) {
	t.Parallel()
	for _, difficulty := range []uint8{1, 2} {
		zombies, _ := reinforcementRun(t, difficulty, 100)
		if len(zombies) != 1 {
			t.Errorf("difficulty %d: %d zombies stood after a hurt, want the struck one alone", difficulty, len(zombies))
		}
	}
}

// TestAHurtZombieCallsNothingWhereMonstersDoNotSpawn checks the game rules
// behind the call: spawn_mobs and spawn_monsters each stop it.
// vanilla: ServerLevel.isSpawningMonsters
func TestAHurtZombieCallsNothingWhereMonstersDoNotSpawn(t *testing.T) {
	t.Parallel()
	for _, rule := range []string{world.RuleSpawnMobs.String(), world.RuleSpawnMonsters.String()} {
		zombies, _ := reinforcementRun(t, 3, 100, rule)
		if len(zombies) != 1 {
			t.Errorf("%s off: %d zombies stood after a hurt, want the struck one alone", rule, len(zombies))
		}
	}
}

// TestAZombieThatDrownsRollsItsAttributesAgain checks the conversion's
// handleAttributes: the drowned a zombie turns into carries a call chance of
// its own, where an unrolled body would carry none.
// vanilla: Zombie.convertToZombieType
func TestAZombieThatDrownsRollsItsAttributesAgain(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Diver"))
	srv.SetSpawning(false)
	srv.SetDayTime(18000)
	c.JoinAs("Diver")

	// The countdown runs only with the eyes in water; out of it the
	// conversion is cancelled. vanilla: ConversionTracker.tick
	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 67; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	c.ChatCommand("summon minecraft:zombie 3.5 64 0.5 {DrownedConversionTime:1}")
	// The tracker reads this tick's eyes, so a body summoned underwater
	// converts on its second tick rather than cancelling on its first.
	var drowned int32
	servertest.WaitFor(t, srv, 20, func() bool {
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:drowned" {
				drowned = m.ID
			}
		}
		return drowned != 0
	})
	if _, _, chance, _ := srv.MobLeaderState(drowned); chance <= 0 {
		t.Errorf("the converted drowned has a call chance of %v, want a rolled base above nothing", chance)
	}
}

// TestALeaderZombieKeepsItsTraitsThroughASave checks the saved attributes
// and CanBreakDoors: a leader comes back at its boosted health with the
// leader's call chance and its doors.
// vanilla: Zombie.addAdditionalSaveData, LivingEntity attributes codec
func TestALeaderZombieKeepsItsTraitsThroughASave(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	srv.SetSpawning(false)
	srv.SetDayTime(18000)
	c := connect().Join("Keeper")

	zombie := srv.SpawnMob("minecraft:zombie", 2, 64, 2)
	srv.SetMobNoAI(zombie)
	srv.MakeMobLeader(zombie)
	id, _ := srv.MobUUID(zombie)
	_, _, wantChance, _ := srv.MobLeaderState(zombie)

	c.Disconnect()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	// Only the modifiers that exist are written, under vanilla's attribute id.
	attrs, _ := nbt.Get[nbt.List](servertest.SavedMobsByUUID(t, cfg.LevelName)[uuid.MustParse(id)], "attributes")
	seen := map[string]bool{}
	for _, it := range attrs.Items {
		a := it.(nbt.Compound)
		aid, _ := nbt.Get[nbt.String](a, "id")
		seen[string(aid)] = true
		mods, _ := nbt.Get[nbt.List](a, "modifiers")
		for _, mi := range mods.Items {
			m := mi.(nbt.Compound)
			if amount, _ := nbt.Get[nbt.Double](m, "amount"); amount == 0 {
				t.Errorf("%s saved a zero modifier %v", aid, m)
			}
			if mid, _ := nbt.Get[nbt.String](m, "id"); mid == "minecraft:reinforcement_caller_charge" {
				t.Errorf("a zombie that called no reinforcement saved %v", m)
			}
		}
	}
	if !seen["minecraft:spawn_reinforcements"] || seen["minecraft:spawn_reinforcements_chance"] {
		t.Errorf("saved attribute ids %v, want minecraft:spawn_reinforcements and no _chance", seen)
	}

	connect().Join("Keeper")
	var reloaded int32
	servertest.WaitFor(t, srv, 400, func() bool {
		for _, m := range srv.MobsInDimension(world.DimensionName) {
			if m.UUID == id {
				reloaded = m.ID
			}
		}
		return reloaded != 0
	})
	health, maxHealth, chance, doors := srv.MobLeaderState(reloaded)
	if health != 80 || maxHealth != 80 {
		t.Errorf("the reloaded leader has %v/%v health, want 80/80", health, maxHealth)
	}
	if math.Abs(chance-wantChance) > 1e-9 || chance < 0.6 {
		t.Errorf("the reloaded leader's call chance is %v, want the saved %v", chance, wantChance)
	}
	if !doors {
		t.Error("the reloaded leader lost its door breaking")
	}
}
