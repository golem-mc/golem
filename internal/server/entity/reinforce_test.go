package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// hardWorld is a test world playing on Hard.
func hardWorld() testWorld {
	w := newTestWorld()
	w.diffUp, w.diff = true, rules.DifficultyHard
	return w
}

// hurtAndTick lands a player's blow on the zombie and ticks it once,
// returning the reinforcement calls that tick made.
func hurtAndTick(m *Mob, w World) []MobEvent {
	m.TakeDamage(1, Player{EntityID: 7, X: 2, Y: 64, Z: 0.5})
	var calls []MobEvent
	for _, ev := range m.Tick(w, nil) {
		if ev.Kind == MobCalledReinforcement {
			calls = append(calls, ev)
		}
	}
	return calls
}

// TestAHurtZombieOnHardRollsItsReinforcementChance checks Zombie.hurtServer:
// the call is rolled against the attribute, names the attacker as the
// target, and never comes on Easy or Normal or without a hurt.
func TestAHurtZombieOnHardRollsItsReinforcementChance(t *testing.T) {
	z := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	z.SetReinforcementBase(1)
	calls := hurtAndTick(z, hardWorld())
	if len(calls) != 1 || calls[0].Target != 7 {
		t.Fatalf("a hurt zombie certain to call made %v, want one call against player 7", calls)
	}

	for _, diff := range []uint8{rules.DifficultyPeaceful, rules.DifficultyEasy, rules.DifficultyNormal} {
		w := newTestWorld()
		w.diffUp, w.diff = true, diff
		z := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
		z.SetReinforcementBase(1)
		if calls := hurtAndTick(z, w); len(calls) != 0 {
			t.Errorf("difficulty %d: a hurt zombie called %v, want none", diff, calls)
		}
	}

	z = NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	z.SetReinforcementBase(1)
	for _, ev := range z.Tick(hardWorld(), nil) {
		if ev.Kind == MobCalledReinforcement {
			t.Error("an unhurt zombie called a reinforcement")
		}
	}

	z = NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	if calls := hurtAndTick(z, hardWorld()); len(calls) != 0 {
		t.Errorf("a zombie with no chance called %v", calls)
	}
}

// TestTheReinforcementRollFollowsTheChance checks the share of hurts that
// call against a chance of a quarter.
func TestTheReinforcementRollFollowsTheChance(t *testing.T) {
	const n = 8000
	got := 0
	for seed := range int64(n) {
		z := NewMob(TypeZombie, 1, 0.5, 64, 0.5, seed)
		z.SetReinforcementBase(0.25)
		got += len(hurtAndTick(z, hardWorld()))
	}
	if share := float64(got) / n; math.Abs(share-0.25) > 3*math.Sqrt(0.25*0.75/n) {
		t.Errorf("%.4f of hurts called, want the 0.25 chance", share)
	}
}

// TestACallCostsCallerAndCalleeAFiftieth checks the 0.05 each side loses,
// the chance clamped at nothing.
func TestACallCostsCallerAndCalleeAFiftieth(t *testing.T) {
	caller := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	caller.SetReinforcementBase(0.5)
	caller.ChargeReinforcement()
	if got := caller.ReinforcementChance(); math.Abs(got-0.45) > 1e-9 {
		t.Errorf("the caller's chance is %v after a call, want 0.45", got)
	}
	callee := NewMob(TypeZombie, 2, 0, 64, 0, 2)
	callee.SetReinforcementBase(0.5)
	callee.AsReinforcement(7)
	if got := callee.ReinforcementChance(); math.Abs(got-0.45) > 1e-6 {
		t.Errorf("the callee's chance is %v, want 0.45", got)
	}
	if callee.TargetID != 7 {
		t.Errorf("the callee hunts %d, want 7", callee.TargetID)
	}
	callee.SetReinforcementBase(0)
	if got := callee.ReinforcementChance(); got != 0 {
		t.Errorf("a charged chance of nothing reads %v, want the attribute's floor of 0", got)
	}
}

// TestAZombiesFinalizeRollsItsSpawnAttributes checks Zombie.handleAttributes
// over seeded spawns at the full special multiplier: the base chance under a
// tenth, the knockback bonus under a twentieth, the follow-range bonus past
// 1 a third of the time, the leader in a twentieth with its chance and
// health, and the door breaking a tenth plus the leaders.
func TestAZombiesFinalizeRollsItsSpawnAttributes(t *testing.T) {
	const n = 20000
	var leaders, doors, follows int
	for seed := range int64(n) {
		z := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		z.FinalizeSpawn(GoDice{z.Rand}, testBiome("minecraft:plains"), nil, hard())
		leader := z.zombie.LeaderBonus > 0
		base := z.zombie.ReinforceBase
		if base < 0 || base >= 0.1 {
			t.Fatalf("seed %d: base chance %v outside [0, 0.1)", seed, base)
		}
		if k := z.KnockbackResistance(); k < 0 || k >= 0.05 {
			t.Fatalf("seed %d: knockback resistance %v outside [0, 0.05)", seed, k)
		}
		if z.zombie.FollowBonus != 0 {
			follows++
			if z.zombie.FollowBonus <= 1 || z.zombie.FollowBonus >= 1.5 {
				t.Fatalf("seed %d: follow bonus %v outside (1, 1.5)", seed, z.zombie.FollowBonus)
			}
			if want := 35 * (1 + z.followSpawnBonus) * (1 + z.zombie.FollowBonus); z.followRange() != want {
				t.Fatalf("seed %d: follow range %v, want %v", seed, z.followRange(), want)
			}
		} else if want := 35 * (1 + z.followSpawnBonus); z.followRange() != want {
			t.Fatalf("seed %d: unbonused follow range %v, want %v", seed, z.followRange(), want)
		}
		if leader {
			leaders++
			if b := z.zombie.LeaderBonus; b < 0.5 || b >= 0.75 {
				t.Fatalf("seed %d: leader bonus %v outside [0.5, 0.75)", seed, b)
			}
			if z.MaxHealth < 40 || z.MaxHealth >= 100 || z.Health != z.MaxHealth {
				t.Fatalf("seed %d: leader health %v/%v, want a full 40 to 100", seed, z.Health, z.MaxHealth)
			}
			if !z.CanBreakDoors {
				t.Fatalf("seed %d: a leader cannot break doors", seed)
			}
		} else if z.MaxHealth != 20 {
			t.Fatalf("seed %d: non-leader health %v, want 20", seed, z.MaxHealth)
		}
		if z.CanBreakDoors {
			doors++
		}
	}
	check := func(name string, got int, want float64) {
		t.Helper()
		share := float64(got) / n
		if math.Abs(share-want) > 3*math.Sqrt(want*(1-want)/n)+0.003 {
			t.Errorf("%s share %.4f, want %.4f", name, share, want)
		}
	}
	check("leader", leaders, 0.05)
	check("follow bonus", follows, 1.0/3)
	check("door breaking", doors, 0.1+0.05-0.1*0.05)

	for seed := range int64(2000) {
		z := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		z.FinalizeSpawn(GoDice{z.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if z.CanBreakDoors || z.zombie.LeaderBonus != 0 || z.zombie.FollowBonus != 0 || z.MaxHealth != 20 {
			t.Fatalf("seed %d: the special multiplier of none still rolled doors, a leader or a follow bonus", seed)
		}
	}
}

// TestAKnockbackResistantZombieIsShovedLess checks the resistance cuts the
// knockback's power.
func TestAKnockbackResistantZombieIsShovedLess(t *testing.T) {
	plain := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	resistant := NewMob(TypeZombie, 2, 0, 64, 0, 1)
	resistant.zombie.KnockbackResist = 0.04
	plain.knockback(0.4, 1, 0)
	resistant.knockback(0.4, 1, 0)
	if math.Abs(resistant.VX) >= math.Abs(plain.VX) {
		t.Errorf("resistant shove %v not smaller than plain %v", resistant.VX, plain.VX)
	}
	if want := plain.VX * 0.96; math.Abs(resistant.VX-want) > 1e-9 {
		t.Errorf("resistant shove %v, want %v", resistant.VX, want)
	}
}

// reinforcementGround is a spawn world floored with stone and dark enough
// for a monster, the called zombie's stage.
func reinforcementGround(t *testing.T) spawnTestWorld {
	w := newSpawnTestWorld()
	w.diffUp, w.diff = true, rules.DifficultyHard
	for x := int32(-48); x <= 48; x++ {
		for z := int32(-48); z <= 48; z++ {
			w.groundAt(t, x, 63, z)
		}
	}
	return w
}

// TestAReinforcementSiteIsSevenToFortyBlocksOff checks the site search: each
// axis offset is nought or 7 to 40 blocks, no player stands within 7 blocks
// of the site, and a spectator is no obstacle.
func TestAReinforcementSiteIsSevenToFortyBlocksOff(t *testing.T) {
	w := reinforcementGround(t)
	players := []SpawnPlayer{{X: 12, Y: 64, Z: 0}, {X: 0, Y: 64, Z: 20, Spectator: true}}
	found := 0
	for seed := int64(1); seed <= 200; seed++ {
		x, y, z, ok := ReinforcementSite(w, rand.New(rand.NewSource(seed)), TypeZombie, 0, 64, 0, players, nil)
		if !ok {
			continue
		}
		found++
		for _, d := range []int32{x, y - 64, z} {
			if a := max(d, -d); a != 0 && (a < 7 || a > 40) {
				t.Fatalf("seed %d: site (%d,%d,%d) has an axis offset %d outside 7 to 40", seed, x, y, z, d)
			}
		}
		if Dist2(12, 64, 0, float64(x), float64(y), float64(z)) < 49 {
			t.Fatalf("seed %d: site (%d,%d,%d) is within 7 blocks of a player", seed, x, y, z)
		}
	}
	if found < 100 {
		t.Errorf("%d of 200 searches found a site on open ground, want most", found)
	}
}

// TestAReinforcementSiteNeedsAGroundAndADarkPlace checks the spawn rules ride
// the search: no floor, or a lit place, leaves no site.
func TestAReinforcementSiteNeedsAGroundAndADarkPlace(t *testing.T) {
	bare := newSpawnTestWorld()
	bare.diffUp, bare.diff = true, rules.DifficultyHard
	lit := reinforcementGround(t)
	lit.sky = 15
	lit.darken = 0
	for name, w := range map[string]spawnTestWorld{"bare": bare, "lit": lit} {
		for seed := int64(1); seed <= 50; seed++ {
			if x, y, z, ok := ReinforcementSite(w, rand.New(rand.NewSource(seed)), TypeZombie, 0, 64, 0, nil, nil); ok {
				t.Fatalf("%s world: seed %d stood a reinforcement at (%d,%d,%d)", name, seed, x, y, z)
			}
		}
	}
}

// TestAKillingBlowStillCallsAReinforcement checks that a hurt which kills
// rolls its call too.
// vanilla: Zombie.hurtServer
func TestAKillingBlowStillCallsAReinforcement(t *testing.T) {
	z := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	z.SetReinforcementBase(1)
	z.Health = 0.1
	calls := hurtAndTick(z, hardWorld())
	if !z.Dead || len(calls) != 1 || calls[0].Target != 7 {
		t.Errorf("dead %v, calls %v after a killing blow, want one call against player 7", z.Dead, calls)
	}
}

// TestAZombieOfAMixedGroupStillRollsItsAttributes checks that a pack an
// animal claimed first leaves the door roll out but not handleAttributes.
// vanilla: Zombie.finalizeSpawn
func TestAZombieOfAMixedGroupStillRollsItsAttributes(t *testing.T) {
	z := NewMob(TypeZombie, 1, 0, 64, 0, 5)
	z.FinalizeSpawn(GoDice{z.Rand}, testBiome("minecraft:plains"), &SpawnGroup{claimed: true}, rules.LocalDifficulty{Special: 1})
	if z.zombie.ReinforceBase <= 0 || z.zombie.KnockbackResist <= 0 {
		t.Errorf("a zombie of an animal's pack rolled base %v, resistance %v, want both above nothing",
			z.zombie.ReinforceBase, z.zombie.KnockbackResist)
	}
}

// TestAConversionRollsAgainAndKeepsTheHealth checks the conversion's roll:
// it fills the traits in, and a leader keeps the health it carried.
// vanilla: Zombie.convertToZombieType, Zombie.handleAttributes
func TestAConversionRollsAgainAndKeepsTheHealth(t *testing.T) {
	leaders := 0
	for seed := range int64(400) {
		d := NewMob(TypeDrowned, 1, 0, 64, 0, seed)
		d.Health = 5
		d.RollConversionAttributes(1)
		if d.zombie.ReinforceBase <= 0 {
			t.Fatalf("seed %d: the conversion left the call chance unrolled", seed)
		}
		if d.zombie.LeaderHealth == 0 {
			continue
		}
		leaders++
		if d.Health != 5 || d.MaxHealth <= 20 {
			t.Fatalf("seed %d: leader health %v/%v after a conversion, want 5 of a boosted maximum", seed, d.Health, d.MaxHealth)
		}
	}
	if leaders == 0 {
		t.Error("no conversion rolled a leader in 400 tries")
	}
}

// TestADeadPlayerIsNoNeighbourOfAReinforcement checks the near-player rule
// skips the dead.
// vanilla: EntityGetter.hasNearbyAlivePlayer
func TestADeadPlayerIsNoNeighbourOfAReinforcement(t *testing.T) {
	if playerNear([]SpawnPlayer{{X: 1, Y: 64, Z: 0, Dead: true}}, 0, 64, 0, 7) {
		t.Error("a dead player counted as near")
	}
	if !playerNear([]SpawnPlayer{{X: 1, Y: 64, Z: 0}}, 0, 64, 0, 7) {
		t.Error("a living player did not count as near")
	}
}
