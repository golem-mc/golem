package entity

import (
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
