package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// The hostile machinery as ordinary unit tests, the way the passive mob's
// walking and wandering is: each case steps a zombie against a world that is
// nothing but the blocks and the weather it sets, with the players as the
// plain list the tick is handed, and asserts on the decisions vanilla's
// goals and despawn run on. The black-box tests in internal/server/server_hostile_
// test.go remain the proof of the whole pipeline over the wire.

// hunter is a zombie stood on a stone floor at the middle of the map, with
// its goals left wired the way a spawned one comes.
func hunter() *Mob {
	return NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
}

func TestMeleeReachUsesTargetBox(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	golem := NewMob(TypeIronGolem, 2, 1.7, 0, 0, 4)
	wolf := NewMob(TypeWolf, 3, 0, 1, 0, 5)
	rabbit := NewMob(TypeRabbit, 4, 0, 0, 0, 6)
	for _, tc := range []struct {
		name   string
		mob    *Mob
		target Player
		want   bool
	}{
		{"wide target inside reach", zombie, golem.asTarget(), true},
		{"short target below attacker", wolf, rabbit.asTarget(), false},
		{"default player inside reach", zombie, Player{X: 1.4}, true},
		{"default player outside reach", zombie, Player{X: 1.5}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mob.inMeleeReach(&tc.target); got != tc.want {
				t.Errorf("inMeleeReach = %v, want %v", got, tc.want)
			}
		})
	}
	golem.X = 1.9
	target := golem.asTarget()
	if zombie.inMeleeReach(&target) {
		t.Error("golem at x=1.9 is outside melee reach")
	}
}

// floor64 lays the stone floor the tests stand their mobs on.
func floor64(w testWorld, t testing.TB) {
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, stone)
		}
	}
}

// survivor is the player the tests put in the world: huntable, five blocks
// east of the zombie, alive.
func survivor(id int32, x float64) Player {
	return Player{X: x, Y: 64, Z: 0.5, EntityID: id, Huntable: true}
}

// TestAZombieTargetsTheNearestHuntablePlayer checks the hunt's first half: a
// zombie with a survival player inside its follow range finds them, holds
// their entity ID as its target, and walks to them.
func TestAZombieTargetsTheNearestHuntablePlayer(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 5.5)}

	sawTarget := false
	for range 60 {
		z.Tick(w, players)
		if z.TargetID == 7 {
			sawTarget = true
			break
		}
	}
	if !sawTarget {
		t.Fatalf("the zombie never took the player as its target in sixty ticks")
	}
	if z.X >= 3 {
		t.Errorf("the zombie stands at x=%v after finding its target, want it walking east", z.X)
	}
}

// TestAZombieNeverTargetsTheModesItMayNotHunt checks the targeting
// conditions' other half: a creative player and a dead one stand in the
// zombie's follow range and the zombie stays idle.
// vanilla: canBeSeenAsEnemy
func TestAZombieNeverTargetsTheModesItMayNotHunt(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{
		{X: 3.5, Y: 64, Z: 0.5, EntityID: 2, Huntable: false},
		{X: 4.5, Y: 64, Z: 0.5, EntityID: 3, Huntable: false},
	}
	for range 200 {
		z.Tick(w, players)
		if z.TargetID != 0 {
			t.Fatalf("the zombie took entity %d as its target, want none of the unhuntable", z.TargetID)
		}
	}
}

// TestAZombieNamesThePlayerItsEyesRankNearest holds the hunt's choice of
// target to vanilla's own ranking: the scan ranks the candidates by the
// distance from the mob's eye point to each player's feet
// (NearestAttackableTargetGoal.findTarget's anchor), not by the feet-to-feet
// distance, so of a player five deep in a shaft and one on the floor six
// out, it is the one on the floor the zombie names.
func TestAZombieNamesThePlayerItsEyesRankNearest(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	// The shaft two cells east of the zombie's own cell, five deep: the
	// sample steps of the sight line clear its mouth at that rise, so the
	// player at its bottom stands in full view.
	for x := int32(2); x <= 3; x++ {
		for y := int32(59); y <= 63; y++ {
			w.set(x, y, 0, 0)
		}
	}
	z := NewMob(TypeZombie, 1, 1.5, 64, 0.5, 3)
	players := []Player{
		{X: 3.5, Y: 59, Z: 0.5, EntityID: 7, Huntable: true}, // down the shaft, 5.4 off by the feet
		{X: 7.5, Y: 64, Z: 0.5, EntityID: 8, Huntable: true}, // on the floor, 6.0 off by the feet
	}
	if !z.seesPlayer(w, 3.5, 59, 0.5) {
		t.Fatal("the player down the shaft is out of the zombie's sight, want both candidates seen")
	}
	for range 60 {
		z.Tick(w, players)
		if z.TargetID != 0 {
			break
		}
	}
	if z.TargetID != 8 {
		t.Errorf("a zombie between two players took target %d, want the one on the floor its eye point ranks nearer", z.TargetID)
	}
}

// TestAZombieDropsATargetThatLeaves checks the target's other edge: a target
// that walks out of the follow range, or out of the world's list altogether,
// is dropped and the arms come down with the hunt.
func TestAZombieDropsATargetThatLeaves(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 5.5)}
	for range 60 {
		z.Tick(w, players)
	}
	if z.TargetID != 7 {
		t.Fatalf("the zombie never took its target")
	}

	// Far away, but still in the list: beyond the follow range the hunt
	// lets the target go.
	far := survivor(7, 200)
	players[0] = far
	for range 5 {
		z.Tick(w, players)
	}
	if z.TargetID != 0 {
		t.Errorf("a target past the follow range was kept, want it dropped")
	}
	if z.Aggressive {
		t.Error("a zombie whose hunt ended is still aggressive, want its arms down")
	}

	// Out of the list entirely — the character screen, another dimension —
	// drops the target the same way.
	players = []Player{survivor(7, 5.5)}
	for range 60 {
		z.Tick(w, players)
	}
	for range 5 {
		z.Tick(w, nil)
	}
	if z.TargetID != 0 {
		t.Errorf("a target gone from the world was kept, want it dropped")
	}
}

// TestAZombieSwingsWhenTheTargetIsInReach checks the blow: with the target
// against the zombie, the melee goal lands its type's own damage on the
// event the server's pipeline takes, and the cooldown keeps the next swing
// a full interval away.
func TestAZombieSwingsWhenTheTargetIsInReach(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 1.1)}

	var swings []MobEvent
	for range 100 {
		for _, ev := range z.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				swings = append(swings, ev)
			}
		}
	}
	if len(swings) == 0 {
		t.Fatal("a zombie with its target against it never swung")
	}
	for _, ev := range swings {
		if ev.Damage != 3 {
			t.Errorf("a zombie's blow carried %v, want its attack damage of 3", ev.Damage)
		}
		if ev.Player.EntityID != 7 {
			t.Errorf("a zombie's blow landed on entity %d, want its target", ev.Player.EntityID)
		}
	}
	if len(swings) > 6 {
		t.Errorf("a hundred ticks produced %d swings, want vanilla's twenty-tick interval to hold them to six",
			len(swings))
	}
	// The first swing comes inside the first handful of ticks: a zombie
	// whose target stands against it does not wind up for a second.
	if swings[0].Damage != 3 {
		t.Error("the first swing was not the zombie's own")
	}
}

// TestAZombiesBlowTakesItsHeldWeaponsDamage holds the swing's damage to the
// main hand: the kind's own three plus the item's attack_damage modifier —
// the iron sword's five, the shovel's three and a half and the spear's two,
// the tools the spawn roll arms a zombie with,
// Zombie.populateDefaultEquipmentSlots.
func TestAZombiesBlowTakesItsHeldWeaponsDamage(t *testing.T) {
	for _, tc := range []struct {
		held string
		want float64
	}{
		{"", 3},
		{"minecraft:iron_sword", 8},
		{"minecraft:iron_shovel", 6.5},
		{"minecraft:iron_spear", 5},
	} {
		z := hunter()
		if tc.held != "" {
			z.MainHand = protocol.ItemStack{ItemID: itemID(t, tc.held), Count: 1}
		}
		if got := z.attackDamage(); got != tc.want {
			t.Errorf("a zombie holding %q swings for %v, want %v", tc.held, got, tc.want)
		}
	}
}

// TestAZombieCannotHitThroughAWall checks the line of sight the melee swing
// asks for: a target sealed into a box — no way in, no sight through — is
// never reached by a blow, however long the zombie beats on the stone.
func TestAZombieCannotHitThroughAWall(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	// A sealed box around the target: walls four high and a roof, floor
	// and all, with no gap to walk through or see through.
	for dx := int32(2); dx <= 5; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			for y := int32(64); y <= 68; y++ {
				if dx == 2 || dx == 5 || dz == -2 || dz == 2 || y == 64 || y == 68 {
					w.set(dx, y, dz, stone)
				}
			}
		}
	}
	z := hunter()
	players := []Player{survivor(7, 3.5)}

	swings := 0
	for range 200 {
		for _, ev := range z.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				swings++
			}
		}
	}
	if swings != 0 {
		t.Errorf("a zombie facing a sealed box swung %d times, want the sight line to keep its blows down",
			swings)
	}
}

// TestABurningZombieBurnsInTheDaylightAndGoesOutInTheWater checks the sun:
// a zombie under an open sky in the burning daylight catches fire, the fire
// bites a point at vanilla's rhythm, and the water puts the whole thing
// out.
func TestABurningZombieBurnsInTheDaylightAndGoesOutInTheWater(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 0
	floor64(w, t)
	z := hunter()

	ignited := false
	for range 200 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			ignited = true
			break
		}
	}
	if !ignited {
		t.Fatal("a zombie in full daylight under an open sky never caught fire")
	}

	// The fire bites once every twentieth tick of it: over the eight
	// seconds of the burn, eight bites land, each the whole point of fire
	// damage — on_fire is on the bypasses-armor tag, so the zombie's two
	// armour points leave it alone. The daylight attribute is dropped for
	// the count: while it stands the sun rolls the fire afresh each tick.
	w.burn = false
	wounded := 0
	for range 160 {
		for _, ev := range z.Tick(w, nil) {
			if ev.Kind == MobBurned {
				wounded++
			}
		}
	}
	if wounded != 8 {
		t.Errorf("a full burn hurt the zombie %d times, want vanilla's one a second eight times", wounded)
	}
	if !closeEnough(z.Health, z.MaxHealth-8) {
		t.Errorf("the burn left the zombie at %v of %v, want eight whole bites off", z.Health, z.MaxHealth)
	}

	// And the water puts it out whole.
	water := defaultState(t, "minecraft:water")
	w.set(0, 64, 0, water)
	z.Tick(w, nil)
	if z.FireTicks != 0 {
		t.Errorf("the zombie kept %d fire ticks in water, want the water to put it out", z.FireTicks)
	}
}

// TestAZombieInTheShadeDoesNotBurn checks the other half of the sun: under a
// roof — no sky light at the eye — the same burning daylight lights nothing,
// and neither does a night under an open sky.
func TestAZombieInTheShadeDoesNotBurn(t *testing.T) {
	// Under a roof: the sky light the burn reads is nothing.
	w := newTestWorld()
	w.burn = true
	w.sky = 0
	floor64(w, t)
	z := hunter()
	for range 400 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			t.Fatal("a zombie under a roof caught fire")
		}
	}

	// In the dark of night, under an open sky: the attribute is down.
	night := newTestWorld()
	floor64(night, t)
	z2 := hunter()
	for range 400 {
		z2.Tick(night, nil)
		if z2.FireTicks > 0 {
			t.Fatal("a zombie under a night sky caught fire")
		}
	}
}

// TestARawLightOfTwelveStillBurns checks the curve's own crossing: the
// float32 light-level magic value sits a hair past half at a raw light of
// twelve, so a zombie under the dusk's darkening of three still catches.
func TestARawLightOfTwelveStillBurns(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 3
	floor64(w, t)
	z := hunter()

	ignited := false
	for range 20000 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			ignited = true
			break
		}
	}
	if !ignited {
		t.Error("a zombie at a raw light of twelve never caught fire, want the float32 curve to light it")
	}
}

// TestATorchLitZombieUnderARoofDoesNotBurn checks the sun's sky half against
// block light: the magic value reads the brighter of block and sky light, but
// the burn needs the eye cell to see the sky, so a roofed zombie beside a
// torch at a bright raw light stays unlit.
func TestATorchLitZombieUnderARoofDoesNotBurn(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 0
	w.block = 14
	floor64(w, t)
	z := hunter()
	for range 400 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			t.Fatal("a torch-lit zombie under a roof caught fire, want the sky gate to spare it")
		}
	}
}

// TestAZombieVanishesOnPeaceful checks the difficulty's word: a hostile mob
// standing in a peaceful world is discarded at once, with the event that
// says so and no death beside it.
func TestAZombieVanishesOnPeaceful(t *testing.T) {
	w := newTestWorld()
	w.diff = rules.DifficultyPeaceful
	w.diffUp = true
	floor64(w, t)
	z := hunter()
	vanished := false
	for range 5 {
		for _, ev := range z.Tick(w, nil) {
			if ev.Kind == MobVanished {
				vanished = true
			}
			if ev.Kind == MobDied {
				t.Error("a peacefully removed zombie died, want it discarded whole")
			}
		}
	}
	if !vanished {
		t.Error("a zombie in a peaceful world was never removed")
	}
}

// TestAZombieFarFromEveryPlayerIsRemovedAtOnce checks the despawn distance:
// a non-persistent zombie more than the MONSTER category's 128 blocks from
// every player is gone the tick it is measured, while one a player stands
// beside stays.
func TestAZombieFarFromEveryPlayerIsRemovedAtOnce(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{{X: 200, Y: 64, Z: 0.5, EntityID: 9, Huntable: true}}
	vanished := false
	for range 5 {
		for _, ev := range z.Tick(w, players) {
			if ev.Kind == MobVanished {
				vanished = true
			}
		}
	}
	if !vanished {
		t.Error("a zombie two hundred blocks from everyone was kept, want it removed at once")
	}

	// And a player standing beside it keeps it.
	kept := hunter()
	players = []Player{survivor(9, 1.5)}
	for range 20 {
		for _, ev := range kept.Tick(w, players) {
			if ev.Kind == MobVanished {
				t.Error("a zombie a player stands beside was removed")
			}
		}
	}
}

// TestASpectatorDoesNotResetTheIdleCounter checks that the despawn's
// nearest-player measurement leaves a spectator out: beside a spectator, and
// with the only non-spectator player past the no-despawn distance, the
// idleness counter still grows instead of being wound back to nought.
// vanilla: Mob.checkDespawn's getNearestPlayer with EntitySelector.NO_SPECTATORS
func TestASpectatorDoesNotResetTheIdleCounter(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	spectator := Player{X: 1.5, Y: 64, Z: 0.5, EntityID: 8, Huntable: false, Spectator: true}
	players := []Player{spectator, survivor(9, 40.5)}
	for range 5 {
		z.Tick(w, players)
	}
	if z.NoActionTicks == 0 {
		t.Error("a spectator beside the zombie wound the idleness counter back to nought")
	}
}

// TestAZombieRetaliatesOnWhoeverStruckIt checks the grudge: a zombie struck
// by a huntable player turns on them the next tick, ahead of any nearer
// stranger, and a strike from a mode it may not hunt leaves it idle.
func TestAZombieRetaliatesOnWhoeverStruckIt(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	attacker := survivor(7, 5.5)
	z.TakeDamage(1, attacker)
	players := []Player{attacker, survivor(8, 1.2)}
	for range 5 {
		z.Tick(w, players)
	}
	if z.TargetID != 7 {
		t.Errorf("a struck zombie targeted entity %d, want the one that hit it", z.TargetID)
	}
	if panicking(z) {
		t.Error("a struck zombie panicked, want the hostile mind to hold its ground")
	}
	// A punch from a creative player earns nothing: the mode may not be
	// hunted, so the grudge is dropped at the first look.
	struck := hunter()
	creative := Player{X: 5.5, Y: 64, Z: 0.5, EntityID: 3, Huntable: false}
	struck.TakeDamage(1, creative)
	players = []Player{creative}
	for range 5 {
		struck.Tick(w, players)
	}
	if struck.TargetID != 0 {
		t.Errorf("a zombie struck by an unhuntable player targeted entity %d, want nobody", struck.TargetID)
	}
}

// TestAZombieAnswersAnotherMobsBlowAndAlertsItsNeighbours checks the grudge
// against a mob attacker and the alert: a zombie wounded by a skeleton's
// arrow names the skeleton for its target, and the zombies of its follow
// range that hunt nobody are sent after the skeleton too.
func TestAZombieAnswersAnotherMobsBlowAndAlertsItsNeighbours(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	shooter := NewMob(TypeSkeleton, 2, 9.5, 64, 0.5, 3)
	neighbour := NewMob(TypeZombie, 3, 3.5, 64, 3.5, 4)
	others := []*Mob{shooter, neighbour}

	z.HurtByArrow(1, 9.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		z.tick(w, nil, others)
	}
	if z.TargetID != shooter.ID {
		t.Errorf("a zombie struck by a skeleton's arrow targeted %d, want the skeleton %d", z.TargetID, shooter.ID)
	}
	if neighbour.TargetID != shooter.ID {
		t.Errorf("the alerted neighbour targeted %d, want the skeleton %d", neighbour.TargetID, shooter.ID)
	}
}

// TestADrownedIgnoresAnotherDrownedsBlow checks vanilla's exclusion: the
// drowned's HurtByTargetGoal is built with Drowned.class, so a drowned's
// arrow earns no grudge and no target.
func TestADrownedIgnoresAnotherDrownedsBlow(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	d := NewMob(TypeDrowned, 1, 0.5, 64, 0.5, 3)
	other := NewMob(TypeDrowned, 2, 5.5, 64, 0.5, 3)
	d.HurtByArrow(1, 5.5, 0.5, 0, other.ID, false, false)
	for range 10 {
		d.tick(w, nil, []*Mob{other})
	}
	if d.TargetID != 0 {
		t.Errorf("a drowned struck by another drowned targeted %d, want the blow ignored", d.TargetID)
	}
}

// TestAnAlertedNeighbourLetsGoOfAGoneAttacker checks that an alerted target
// has an owner: the neighbour's own goal adopts the attacker the alert named
// and, once the attacker dies with a survival player in view, drops it and
// takes up the player rather than holding a dead body for good.
func TestAnAlertedNeighbourLetsGoOfAGoneAttacker(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	struck := hunter()
	shooter := NewMob(TypeSkeleton, 2, 9.5, 64, 0.5, 3)
	neighbour := NewMob(TypeZombie, 3, 3.5, 64, 3.5, 4)
	others := []*Mob{shooter, neighbour}

	struck.HurtByArrow(1, 9.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		struck.tick(w, nil, others)
	}
	if neighbour.TargetID != shooter.ID {
		t.Fatalf("the neighbour was not alerted: target %d, want the skeleton %d", neighbour.TargetID, shooter.ID)
	}
	// The neighbour's own goal takes the alert up while the attacker lives.
	for range 5 {
		neighbour.tick(w, nil, others)
	}
	if neighbour.TargetID != shooter.ID {
		t.Fatalf("the neighbour let a live attacker go: target %d, want %d", neighbour.TargetID, shooter.ID)
	}

	// The attacker dies with a survival player in plain view: the target
	// goes, and the nearest hunt takes the player up.
	shooter.Dead = true
	player := survivor(7, 2.5)
	for range 400 {
		neighbour.tick(w, []Player{player}, others)
	}
	if neighbour.TargetID != player.EntityID {
		t.Errorf("after the attacker died the neighbour targeted %d, want the player %d", neighbour.TargetID, player.EntityID)
	}
}

// TestAnAlertCallsOnlyTheStruckKindsClass checks vanilla's
// getEntitiesOfClass(this.mob.getClass()): the alert reaches the struck
// kind's own class and its subclasses. A struck husk calls only husks, while
// a struck zombie calls its whole family — the husk, the drowned and the
// zombie villager.
func TestAnAlertCallsOnlyTheStruckKindsClass(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	shooter := NewMob(TypeSkeleton, 9, 0.5, 64, 0.5, 3)

	husk := NewMob(TypeHusk, 1, 0.5, 64, 0.5, 3)
	fellowHusk := NewMob(TypeHusk, 3, 3.5, 64, 3.5, 4)
	plainZombie := NewMob(TypeZombie, 4, 2.5, 64, 2.5, 5)
	husk.HurtByArrow(1, 0.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		husk.tick(w, nil, []*Mob{shooter, fellowHusk, plainZombie})
	}
	if fellowHusk.TargetID != shooter.ID {
		t.Errorf("a struck husk did not alert the husk beside it: target %d, want %d", fellowHusk.TargetID, shooter.ID)
	}
	if plainZombie.TargetID != 0 {
		t.Errorf("a struck husk alerted a plain zombie: target %d, want none", plainZombie.TargetID)
	}

	zombie := NewMob(TypeZombie, 5, 0.5, 64, 0.5, 6)
	fellowHusk2 := NewMob(TypeHusk, 6, 3.5, 64, 3.5, 7)
	fellowDrowned := NewMob(TypeDrowned, 7, 2.5, 64, 2.5, 8)
	fellowVillager := NewMob(TypeZombieVillager, 8, 1.5, 64, 1.5, 10)
	zombie.HurtByArrow(1, 0.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		zombie.tick(w, nil, []*Mob{shooter, fellowHusk2, fellowDrowned, fellowVillager})
	}
	for _, o := range []*Mob{fellowHusk2, fellowDrowned, fellowVillager} {
		if o.TargetID != shooter.ID {
			t.Errorf("a struck zombie did not alert a family %s: target %d, want %d", o.Type, o.TargetID, shooter.ID)
		}
	}
}

// TestAnAlertSearchesVanillasBox checks the alert's search volume: vanilla
// inflates the struck mob's foot cube by the follow range on x and z but only
// ten blocks up and down (AABB.unitCubeFromLowerCorner(pos).inflate(follow,
// 10, follow)), so a neighbour straight up is out of reach and one far out on
// the diagonal, past the follow-range sphere, is in.
func TestAnAlertSearchesVanillasBox(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	shooter := NewMob(TypeSkeleton, 9, 0.5, 64, 0.5, 3)

	// Twenty blocks straight up: past the ten the box reaches.
	struck := hunter()
	above := NewMob(TypeZombie, 3, 0.5, 84, 0.5, 4)
	struck.HurtByArrow(1, 0.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		struck.tick(w, nil, []*Mob{shooter, above})
	}
	if above.TargetID != 0 {
		t.Errorf("a zombie twenty blocks above the struck one was alerted: target %d, want none", above.TargetID)
	}

	// Thirty blocks out on both x and z: outside the follow sphere's
	// diagonal, inside the box that stretches the range each way.
	struck2 := NewMob(TypeZombie, 5, 0.5, 64, 0.5, 6)
	diagonal := NewMob(TypeZombie, 6, 30.5, 64, 30.5, 7)
	struck2.HurtByArrow(1, 0.5, 0.5, 0, shooter.ID, false, false)
	for range 5 {
		struck2.tick(w, nil, []*Mob{shooter, diagonal})
	}
	if diagonal.TargetID != shooter.ID {
		t.Errorf("a zombie thirty blocks off on x and z was not alerted: target %d, want %d", diagonal.TargetID, shooter.ID)
	}
}

// TestADrownedAlertsAnotherDrowned checks vanilla's Drowned.registerGoals:
// HurtByTargetGoal(this, Drowned.class).setAlertOthers — a drowned answers no
// other drowned's blow but does call the other drowneds to a stranger's.
func TestADrownedAlertsAnotherDrowned(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	zombie := NewMob(TypeZombie, 5, 9.5, 64, 0.5, 3)
	d := NewMob(TypeDrowned, 1, 0.5, 64, 0.5, 3)
	fellow := NewMob(TypeDrowned, 3, 3.5, 64, 3.5, 4)
	d.HurtByArrow(1, 9.5, 0.5, 0, zombie.ID, false, false)
	for range 5 {
		d.tick(w, nil, []*Mob{zombie, fellow})
	}
	if d.TargetID != zombie.ID {
		t.Errorf("a drowned struck by a zombie targeted %d, want %d", d.TargetID, zombie.ID)
	}
	if fellow.TargetID != zombie.ID {
		t.Errorf("a struck drowned did not alert the drowned beside it: target %d, want %d", fellow.TargetID, zombie.ID)
	}
}

// TestAHurtFromAnotherMobRecordsTheAttacker checks the mob blow's book: the
// health comes off, the knockback runs away from the attacker, and the
// attacker's entity ID is kept for the grudge — with none of the player
// credit a player's swing carries.
func TestAHurtFromAnotherMobRecordsTheAttacker(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	attacker := NewMob(TypeCow, 2, 0.5, 64, 2.5, 3)
	if !cow.HurtByMob(3, attacker) {
		t.Fatal("a mob's blow did not land on a fresh body")
	}
	if cow.Health != cow.MaxHealth-3 {
		t.Errorf("the blow left %v of health, want three off the %v it had", cow.Health, cow.MaxHealth)
	}
	if cow.retaliate != attacker.ID {
		t.Errorf("the blow kept attacker %d, want the one who swung %d", cow.retaliate, attacker.ID)
	}
	if cow.hurtByPlayer || cow.hurtByMelee {
		t.Error("a mob's blow was read as a player's own swing")
	}
}

// TestAZombieSwingsAtAMobItHunts checks the melee half of a mob-on-mob hunt:
// with a skeleton named and in reach, the zombie's attack goal lands its blow
// as the same event a swing at a player is, naming the mob's entity ID.
func TestAZombieSwingsAtAMobItHunts(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	prey := NewMob(TypeSkeleton, 2, 1.1, 64, 0.5, 3)
	z.TargetID = prey.ID

	swung := false
	for range 40 {
		for _, ev := range z.tick(w, nil, []*Mob{prey}) {
			if ev.Kind == MobAttackedPlayer && ev.Player.EntityID == prey.ID {
				swung = true
			}
		}
	}
	if !swung {
		t.Error("a zombie with a skeleton in reach never swung at it")
	}
}

// TestAZombieMoansOnVanillasRhythm checks the idle sound: over long enough
// a stretch the ambient timer's die comes up, the moan is reported, and the
// counter is sent back down so the next one waits behind the eighty ticks
// Mob.getAmbientSoundInterval gives.
func TestAZombieMoansOnVanillasRhythm(t *testing.T) {
	z := hunter()
	moans := 0
	for range 2000 {
		if z.moanStep() {
			moans++
			if z.ambientTimer != -80 {
				t.Fatalf("a moan left the timer at %d, want it back under the eighty", z.ambientTimer)
			}
		}
	}
	if moans < 8 || moans > 25 {
		t.Errorf("two thousand ticks produced %d moans, want vanilla's one-every-few-seconds rhythm", moans)
	}
}

// TestAZombieRaisesItsArmsAheadOfEachBlow checks ZombieAttackGoal's rhythm:
// the arms stay down for the hunt's first ticks, come up once it has run
// five and the swing is under half the attack interval away, and drop again
// on the tick of the blow, when the cooldown starts over at twenty.
func TestAZombieRaisesItsArmsAheadOfEachBlow(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 1.5)}

	huntTicks, raisedAt, blows := 0, 0, 0
	for i := range 200 {
		// The target paces back and forth, so the hunt keeps a path to walk:
		// against a still target it ends with each path and starts afresh.
		players[0].Z = 0.5 + 2*math.Sin(float64(i)/40)
		events := z.Tick(w, players)
		if z.TargetID != 0 {
			huntTicks++
		}
		if z.Aggressive && raisedAt == 0 {
			raisedAt = huntTicks
		}
		for _, ev := range events {
			if ev.Kind != MobAttackedPlayer {
				continue
			}
			blows++
			if z.Aggressive {
				t.Errorf("the arms are still up on the tick of blow %d, want them down for the fresh cooldown", blows)
			}
		}
		if blows >= 2 {
			break
		}
	}
	if blows < 2 {
		t.Fatalf("the zombie landed %d blows on a player beside it, want two", blows)
	}
	if raisedAt == 0 || raisedAt < raiseArmsAfter {
		t.Errorf("the arms came up on hunt tick %d, want no sooner than the %dth", raisedAt, raiseArmsAfter)
	}
}

// TestAZombieWalksToTheFootOfAPillarItCannotClimb checks the place a hunt
// leads to when the target stands somewhere the walker cannot: the player
// on a pillar four high is hunted across the floor — the search that
// cannot reach them keeps the way to its best node — and the zombie stands
// at the pillar's foot below them, too far down for any blow.
func TestAZombieWalksToTheFootOfAPillarItCannotClimb(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -25, -25, 25, 25, 63, stone)
	pillarFor(t, w, 0, 0)
	z := NewMob(TypeZombie, 1, -9.5, 64, 0.5, 3)
	players := []Player{{X: 0.5, Y: 68, Z: 0.5, EntityID: 7, Huntable: true}}

	foot := false
	swings := 0
	for range 300 {
		for _, ev := range z.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				swings++
			}
		}
		if !foot && math.Hypot(z.X-0.5, z.Z-0.5) < 2 {
			foot = true
		}
	}
	if !foot {
		t.Fatalf("the zombie never left its place at (%v, %v), want it walked to the pillar's foot", z.X, z.Z)
	}
	if z.Y != 64 {
		t.Errorf("the hunt stands the zombie at y=%v, want the ground it cannot climb off of", z.Y)
	}
	if swings != 0 {
		t.Errorf("the hunt landed %d blows four blocks up, want them out of reach", swings)
	}
}

// pillarFor stands the pillar of the searches: four blocks of stone at the
// cell given, on the floor the caller laid under it.
func pillarFor(t *testing.T, w testWorld, x, z int32) {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(x, y, z, stone)
	}
}

// TestASkeletonAboveShootsDown stands a skeleton on a pillar with its
// quarry twelve blocks below and lets the hunt run to the shot: the arrow
// the event carries aims down at them, the third-of-height point under
// the shooter's eye whatever the horizontal lift does for the shots
// level ground sees.
func TestASkeletonAboveShootsDown(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 59, z, stone)
		}
	}
	// The pillar the skeleton stands on, taller than the drop a healthy
	// hunter's path takes on easy (eight): one column rising out of the
	// floor, its top a block the shot's line of sight clears.
	for y := int32(60); y <= 71; y++ {
		w.set(0, y, 0, stone)
	}
	s := NewMob(TypeSkeleton, 1, 0.5, 72, 0.5, 3)
	s.FinalizeSpawn(GoDice{s.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{}) // the bow
	players := []Player{{X: 0.5, Y: 60, Z: 4.5, EntityID: 7, Huntable: true}}

	for range 400 {
		for _, ev := range s.Tick(w, players) {
			if ev.Kind != MobShotArrow {
				continue
			}
			if ev.VY >= 0 {
				t.Errorf("a skeleton twelve blocks over its target shot with rise %v, want a falling shot", ev.VY)
			}
			if ev.Y != 72+1.74-ArrowSpawnBelow {
				t.Errorf("the arrow was born at y=%v, want the shooter's eye less its tenth", ev.Y)
			}
			return
		}
	}
	t.Fatal("a skeleton on a pillar never shot at the player below")
}

// TestTheIdleCounterGrowsByTheLightDependentMagicValue checks the idleness
// count's other gear: a tick of standing is one tick of idleness, two more
// only where the light-dependent magic value stands above the half — the
// raw light run through the float32 f/(4−3f) bend. For the nought-ambient
// test world the bend's crossing sits at the twelfth raw light, where the
// float32 rounding lands a hair above the half, and the night's darkened
// sky stays under it.
func TestTheIdleCounterGrowsByTheLightDependentMagicValue(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sky, darken uint8
		want        int
		message     string
	}{
		{"the full light of noon", 15, 0, 3, "want the tick of standing and the two the bright half adds"},
		{"the eleventh light, under the bend's crossing", 11, 0, 1, "want the tick of standing alone"},
		{"the twelfth light, where the float32 bend lands a hair over the half", 12, 0, 3, "want the tick of standing and the two the rounding adds"},
		{"the night's darkened sky", 15, 11, 1, "want the tick of standing alone, the darkened sky under the half"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.sky = tc.sky
			w.darken = int(tc.darken)
			floor64(w, t)
			z := hunter()
			before := z.NoActionTicks
			z.idleStep(w)
			if got := z.NoActionTicks - before; got != tc.want {
				t.Errorf("a tick in %s grew the idleness count by %d, %s", tc.name, got, tc.message)
			}
		})
	}
}

// TestAHuntOnATargetInItsOwnCellPaysNoNoWayPenalty checks the recalculation
// clock at its shortest reach: a target standing in the mob's own cell is a
// way the search finds — an empty path vanilla's moveTo answers true to — so
// the hunt is not charged the fifteen a search that finds nothing adds on
// top.
func TestAHuntOnATargetInItsOwnCellPaysNoNoWayPenalty(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 0.8)}
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	g.planTo(&tickView{w: w, players: players}, &players[0])
	if g.ticksUntilNextPathRecalculation > recalcBase+recalcVary-1 {
		t.Errorf("a hunt on a target in its own cell planned %d ticks ahead, want the base cadence's at most %d with no no-way fifteen",
			g.ticksUntilNextPathRecalculation, recalcBase+recalcVary-1)
	}
}

// TestAMeleeHuntRecalculatesOnlyForATargetItSees checks the sight line the
// recalculation asks for: a target behind a wall is not searched for by a
// goal that follows only what it sees, while one told to chase unseen
// searches anyway. The plan's own record of where it was made is the tell.
func TestAMeleeHuntRecalculatesOnlyForATargetItSees(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 65; y++ {
		for z := int32(-1); z <= 1; z++ {
			w.set(3, y, z, stone)
		}
	}
	players := []Player{survivor(7, 6.5)}

	z := hunter()
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	g.Tick(&tickView{w: w, players: players})
	if g.pathedTargetX != 0 {
		t.Error("a hunt with its target behind a wall planned a search, want the sight line to hold it back")
	}

	z2 := hunter()
	z2.TargetID = 7
	spider := &meleeAttackGoal{m: z2, priority: priorityMelee, followUnseen: true}
	spider.Tick(&tickView{w: w, players: players})
	if spider.pathedTargetX == 0 {
		t.Error("a goal told to chase unseen planned no search through a wall, want the flag to let it")
	}
}

// TestAMeleeHuntSearchesAtItsCanStartForATargetItHasNotSeen checks that the
// opening search is not gated on the sight line: MeleeAttackGoal.canUse plans
// the way to the target without asking whether the mob sees it, so a hunt
// that opens on a target behind a wall still gets a path.
func TestAMeleeHuntSearchesAtItsCanStartForATargetItHasNotSeen(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 65; y++ {
		for z := int32(-1); z <= 1; z++ {
			w.set(3, y, z, stone)
		}
	}
	players := []Player{survivor(7, 6.5)}

	z := hunter()
	z.OnGround = true
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	if !g.CanStart(&tickView{w: w, players: players}) {
		t.Fatal("the opening offer refused a target behind a wall")
	}
	if len(g.pendingPath) == 0 {
		t.Error("a hunt that opened on a target behind a wall planned no search, want the opening search to plan regardless of sight")
	}
}

// TestAMeleeHuntStartsWithNoWayToItsTarget checks MeleeAttackGoal.canUse's
// non-refusal: a target the search can find no way to, standing beyond a
// blow's reach, still starts the hunt — vanilla's createPath is non-null for a
// body that may plan, holding the empty way to the body's own cell.
func TestAMeleeHuntStartsWithNoWayToItsTarget(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	// A ring of stone around the zombie's own cell, two high: no neighbour
	// the search may step to, so every plan to the far target comes back
	// empty.
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			for y := int32(64); y <= 65; y++ {
				w.set(dx, y, dz, stone)
			}
		}
	}
	z := hunter()
	z.OnGround = true
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	if !g.CanStart(&tickView{w: w, players: players}) {
		t.Fatal("a hunt with no way to its target and the target out of reach was refused, want the empty way to the mob's own cell")
	}
	if len(g.pendingPath) != 0 {
		t.Errorf("a sealed hunt planned %d waypoints, want the empty way to its own cell", len(g.pendingPath))
	}
	// The empty way is a spent path: the hunt ends with it, so the arms of a
	// sealed zombie never rise.
	g.Start(&tickView{w: w, players: players})
	if g.ShouldContinue(&tickView{w: w, players: players}) {
		t.Error("a sealed hunt went on past its empty way, want canContinueToUse to end it")
	}
}

// TestAMeleeHuntRefusesAStartItCannotPlan checks createPath's null refusal: a
// body that cannot plan at all — airborne, out of any liquid — with its target
// beyond a blow's reach leaves the hunt unstarted.
func TestAMeleeHuntRefusesAStartItCannotPlan(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	z.OnGround = false
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	if g.CanStart(&tickView{w: w, players: players}) {
		t.Error("an airborne hunt out of reach started, want vanilla's createPath null refusal")
	}
}

// TestARefusedMeleeStartLeavesAnotherGoalsPathStanding checks that a refused
// canUse does not touch the legs: vanilla's canUse keeps its own this.path and
// only start hands a path to the navigation, so a lower-priority MOVE goal's
// path survives the check.
func TestARefusedMeleeStartLeavesAnotherGoalsPathStanding(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	z.OnGround = false // a body that cannot plan, so canUse comes back refused
	z.nav.path = []Waypoint{{X: 1, Y: 64, Z: 0}}
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	if g.CanStart(&tickView{w: w, players: players}) {
		t.Fatal("an airborne hunt out of reach started")
	}
	if len(z.nav.path) == 0 {
		t.Error("a refused start wiped the path another goal was walking, want the legs left alone")
	}
}

// TestADrownedSpendsItsWindowWhenOkTargetRefuses checks the drowned's gate
// order: Drowned$DrownedAttackGoal.canUse runs the melee check first, so the
// twenty-tick window is spent even where okTarget then turns the hunt away —
// the target stepping into water cannot make the next check pass early.
func TestADrownedSpendsItsWindowWhenOkTargetRefuses(t *testing.T) {
	w := newTestWorld()
	w.bright = true // daylight: okTarget wants the target in water
	floor64(w, t)
	z := NewMob(TypeDrowned, 1, 0.5, 64, 0.5, 3)
	z.OnGround = true
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &drownedAttackGoal{meleeAttackGoal{m: z, priority: 2}}
	tick := &tickView{w: w, players: players}
	if g.CanStart(tick) {
		t.Fatal("a drowned took a target out of the water in daylight, want okTarget to refuse it")
	}
	// The target steps into the water; the window the first check spent still
	// holds the drowned back.
	water := defaultState(t, "minecraft:water")
	for y := int32(64); y <= 65; y++ {
		w.set(5, y, 0, water)
	}
	if g.CanStart(tick) {
		t.Error("a drowned started inside the twenty ticks its refused check bought, want the window spent first")
	}
}

// TestAMeleeHuntChecksItsStartOnceEveryTwentyTicks checks MeleeAttackGoal's
// canUse cadence: the opening search runs on the first offer, a second offer
// inside the same twenty game ticks neither searches nor starts, and the
// twentieth tick after the last lets the check through again.
func TestAMeleeHuntChecksItsStartOnceEveryTwentyTicks(t *testing.T) {
	w := &countingWorld{testWorld: newTestWorld()}
	floor64(w.testWorld, t)
	z := hunter()
	z.OnGround = true
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	tick := &tickView{w: w, players: players}
	if !g.CanStart(tick) {
		t.Fatal("the first offer of a start refused a reachable target")
	}
	if w.reads == 0 {
		t.Fatal("the first offer spent no search on the world")
	}
	reads := w.reads
	if g.CanStart(tick) {
		t.Error("a second offer inside the same twenty ticks started the hunt, want the check run once")
	}
	if w.reads != reads {
		t.Errorf("the second offer read the world %d times, want the cadence to spend nothing", w.reads-reads)
	}
	checked := z.Age
	z.Age = checked + canUseCheckInterval - 1
	if g.CanStart(tick) {
		t.Error("an offer nineteen ticks after the last started the hunt, want the window still shut")
	}
	z.Age = checked + canUseCheckInterval
	if !g.CanStart(tick) {
		t.Error("an offer twenty ticks after the last was refused, want the window to open at the twentieth")
	}
}

// TestAMeleeHuntRepathsOnTheFirstTickAfterItsStart checks MeleeAttackGoal's
// start: the re-path clock is reset to nought, so the tick that follows plans
// the way to the target afresh.
func TestAMeleeHuntRepathsOnTheFirstTickAfterItsStart(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 5.5)}
	z.TargetID = 7
	g := &meleeAttackGoal{m: z, priority: priorityMelee}
	tick := &tickView{w: w, players: players}
	g.Start(tick)
	if g.ticksUntilNextPathRecalculation != 0 {
		t.Fatalf("a hunt started with a re-path clock of %d, want the nought MeleeAttackGoal.start resets it to",
			g.ticksUntilNextPathRecalculation)
	}
	g.Tick(tick)
	if g.pathedTargetX != 5.5 {
		t.Errorf("the first tick after the start planned to x=%v, want the target's 5.5", g.pathedTargetX)
	}
}

// countingWorld is a test world that counts the block reads made against
// it, so a test can hold a hunt's searches to their cadence by the work
// they do alone.
type countingWorld struct {
	testWorld
	reads int
}

// BlockAt is the plain test world's read, counted on the way past.
func (w *countingWorld) BlockAt(x, y, z int32) uint16 {
	w.reads++
	return w.testWorld.BlockAt(x, y, z)
}

// BlocksIn counts every cell of the box as a read.
func (w *countingWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}

// huntReadBudget is the most block reads a hunt of a few hundred ticks
// against a target it has no way to may spend: a search reads the ground
// over all around itself, and MeleeAttackGoal's clock lets a hunt that
// keeps finding no way run one only every few dozen ticks — never one a
// tick, which would spend a search's worth of reads on every tick of the
// wait. The figure sits an order below the one-a-tick hunt and a wide
// margin above the cadence's handful of searches.
const huntReadBudget = 400000

// TestAHuntThatFindsNoWayIsNotSearchedEveryTick bounds searches for a visible
// target on a pillar too high to climb.
func TestAHuntThatFindsNoWayIsNotSearchedEveryTick(t *testing.T) {
	w := &countingWorld{testWorld: newTestWorld()}
	floor64(w.testWorld, t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(3, y, 0, stone)
	}
	z := hunter()
	// Stand near the pillar's front edge: its center hides the player's eyes
	// once the zombie reaches the wall, ending the hunt before the budget check.
	// vanilla: LivingEntity.hasLineOfSight, TargetGoal.canContinueToUse
	players := []Player{{X: 3.1, Y: 68, Z: 0.5, EntityID: 7, Huntable: true}}
	p := players[0]

	for tick := range 300 {
		for _, ev := range z.Tick(w, players) {
			if ev.Kind == MobAttackedPlayer {
				t.Fatalf("the zombie reached the player on the pillar at tick %d", tick+1)
			}
		}
		if !z.seesPlayer(w, p.X, p.Y, p.Z) {
			t.Fatalf("the pillar hid the player at tick %d from (%g, %g, %g)", tick+1, z.X, z.Y, z.Z)
		}
		if tick >= 59 && z.TargetID != p.EntityID {
			t.Fatalf("the zombie must acquire and keep the visible player by tick 60; tick %d target = %d", tick+1, z.TargetID)
		}
	}
	t.Logf("the hunt spent %d block reads in 300 ticks (budget %d)", w.reads, huntReadBudget)
	if w.reads > huntReadBudget {
		t.Errorf("the hunt spent %d block reads against an unreachable target in three hundred ticks, want no more than the %d a search on a cadence spends",
			w.reads, huntReadBudget)
	}
}

func burnsWithin(w testWorld, z *Mob, ticks int) bool {
	for range ticks {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			return true
		}
	}
	return false
}

func TestRainInADryBiomeDoesNotSpareTheSunBurn(t *testing.T) {
	w := newTestWorld()
	w.burn, w.sky, w.darken = true, 15, 0
	w.rain, w.dryBiome = true, true
	floor64(w, t)
	if !burnsWithin(w, hunter(), 400) {
		t.Fatal("a zombie in a dry biome did not burn while the world rained")
	}
}

func TestARainOverADryBiomeLeavesTheSunBurnStandingAndHurting(t *testing.T) {
	w := newTestWorld()
	w.burn, w.sky, w.darken = true, 15, 0
	w.rain, w.dryBiome = true, true
	floor64(w, t)
	z := hunter()
	z.Health = z.MaxHealth
	lit := 0
	for range 400 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			lit++
		}
	}
	if lit < 300 || z.Health >= z.MaxHealth {
		t.Fatalf("zombie alight %d of 400 ticks, health %v of %v, want the fire to stand and hurt", lit, z.Health, z.MaxHealth)
	}
}

func TestARoofedBurningZombieKeepsItsFireWhileTheWorldRains(t *testing.T) {
	w := newTestWorld()
	w.rain = true
	floor64(w, t)
	w.set(0, 70, 0, defaultState(t, "minecraft:stone"))
	z := hunter()
	z.FireTicks = 100
	z.Tick(w, nil)
	if z.FireTicks == 0 {
		t.Fatal("a zombie under a roof was put out by rain that does not reach it")
	}
}

func TestRainAtTheZombieSparesTheSunBurn(t *testing.T) {
	w := newTestWorld()
	w.burn, w.sky, w.darken = true, 15, 0
	w.rain = true
	floor64(w, t)
	if burnsWithin(w, hunter(), 400) {
		t.Fatal("a zombie rained on caught fire")
	}
}

func TestPowderSnowSparesTheSunBurnAndOneTickMore(t *testing.T) {
	w := newTestWorld()
	w.burn, w.sky, w.darken = true, 15, 0
	floor64(w, t)
	snow := defaultState(t, "minecraft:powder_snow")
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 64, z, snow)
			w.set(x, 65, z, snow)
		}
	}
	z := hunter()
	if burnsWithin(w, z, 400) {
		t.Fatal("a zombie in powder snow caught fire")
	}
	if z.inPowderSnow(w) != true {
		t.Fatal("zombie not in powder snow; test setup")
	}
	for pos := range w.blocks.cells() {
		if pos[1] >= 64 {
			w.blocks.unset(pos[0], pos[1], pos[2])
		}
	}
	z.Tick(w, nil)
	if z.FireTicks > 0 {
		t.Fatal("a zombie that just left powder snow caught fire on the same tick")
	}
	if !burnsWithin(w, z, 400) {
		t.Fatal("a zombie out of powder snow never burned")
	}
}

// leapGoal finds the leap goal a spider was wired with.
func leapGoal(t *testing.T, m *Mob) *leapAtTargetGoal {
	t.Helper()
	for _, g := range m.ai.goals {
		if l, ok := g.(*leapAtTargetGoal); ok {
			return l
		}
	}
	t.Fatal("a spider was wired without its leap goal")
	return nil
}

// leapsWithin offers the goal the start a hundred times and reports whether
// it ever took it.
func leapsWithin(g *leapAtTargetGoal, view *tickView) bool {
	for range 100 {
		if g.CanStart(view) {
			return true
		}
	}
	return false
}

// TestASpiderLeapsAtATargetTwoToFourBlocksAway checks LeapAtTargetGoal: from
// the ground, a target at three blocks gets the spider an upward and a
// forward velocity; closer than two, further than four, or in the air it
// stays put.
func TestASpiderLeapsAtATargetTwoToFourBlocksAway(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	for _, kind := range []MobType{TypeSpider, TypeCaveSpider} {
		for _, tc := range []struct {
			name     string
			dist     float64
			onGround bool
			want     bool
		}{
			{"three blocks", 3, true, true},
			{"under two blocks", 1.9, true, false},
			{"over four blocks", 4.1, true, false},
			{"in the air", 3, false, false},
		} {
			m := NewMob(kind, 4, 0.5, 64, 0.5, 21)
			m.TargetID = 7
			m.OnGround = tc.onGround
			g := leapGoal(t, m)
			view := &tickView{w: w, players: []Player{survivor(7, 0.5+tc.dist)}}
			if got := leapsWithin(g, view); got != tc.want {
				t.Fatalf("%v, %s: leap started = %v, want %v", kind, tc.name, got, tc.want)
			}
			if !tc.want {
				continue
			}
			g.Start(view)
			if m.VY != 0.4 || m.VX <= 0 || m.VZ != 0 {
				t.Errorf("%v: leap velocity = (%v, %v, %v), want upward 0.4 and forward along +x", kind, m.VX, m.VY, m.VZ)
			}
			if !g.ShouldContinue(view) {
				t.Errorf("%v: leap ended on its first tick", kind)
			}
		}
	}
}

// TestAMeleeHuntEndsWithItsPathUnlessItFollowsOrIsABrain holds
// canContinueToUse: with a target and no path left, the plain hunt stops so
// canUse can plan into the target's cell, while a goal that follows an unseen
// target, and a brain behaviour's stand-in, keep going.
// vanilla: MeleeAttackGoal.canContinueToUse
func TestAMeleeHuntEndsWithItsPathUnlessItFollowsOrIsABrain(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	players := []Player{survivor(7, 3.5)}
	for _, tc := range []struct {
		name string
		goal func(*Mob) *meleeAttackGoal
		want bool
	}{
		{"plain", func(m *Mob) *meleeAttackGoal { return &meleeAttackGoal{m: m} }, false},
		{"follows unseen", func(m *Mob) *meleeAttackGoal { return &meleeAttackGoal{m: m, followUnseen: true} }, true},
		{"brain", func(m *Mob) *meleeAttackGoal { return &meleeAttackGoal{m: m, brain: true} }, true},
	} {
		z := hunter()
		z.TargetID = 7
		g := tc.goal(z)
		tv := &tickView{w: w, players: players}
		if g.ShouldContinue(tv) != tc.want {
			t.Errorf("%s: a hunt with no path left continues = %v, want %v", tc.name, !tc.want, tc.want)
		}
		z.moveTo([]Waypoint{{X: 2, Y: 64, Z: 0}}, Waypoint{X: 3, Y: 64, Z: 0}, 1)
		if !g.ShouldContinue(tv) {
			t.Errorf("%s: a hunt with a path to walk stopped", tc.name)
		}
		z.TargetID = 0
		if g.ShouldContinue(tv) {
			t.Errorf("%s: a hunt with no target continued", tc.name)
		}
	}
}

// arrowAim aims at a third of the target's own height, a player's when unset.
func TestArrowAimUsesTargetHeight(t *testing.T) {
	s := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	for _, c := range []struct{ height, want float64 }{{0.12, 0.12 / 3}, {0.6, 0.2}, {0, 0.6}} {
		p := &Player{X: 10, Y: 64, Height: c.height, Width: 0.36}
		_, oy, _, _, ay, az := arrowAim(s, p, rangedArrowAimLift)
		ax := p.X - s.X
		dist := math.Sqrt(ax*ax + az*az)
		if got := oy + ay - dist*rangedArrowAimLift; math.Abs(got-(64+c.want)) > 1e-9 {
			t.Errorf("height %v: aim y %v, want %v", c.height, got, 64+c.want)
		}
	}
}
