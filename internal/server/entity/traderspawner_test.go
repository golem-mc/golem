package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// traderTestWorld is a spawn test world with one bell, or none.
type traderTestWorld struct {
	spawnTestWorld
	bell *Waypoint
}

func (w traderTestWorld) MeetingPoint(x, y, z int32, r int) (Waypoint, bool) {
	if w.bell == nil {
		return Waypoint{}, false
	}
	return *w.bell, true
}

// tickTrader is one tick of the spawner.
func tickTrader(s *WanderingTraderSpawner, w TraderWorld, rng *rand.Rand, players []SpawnPlayer, st *world.TraderSpawnState, spawn func(TraderSpawn)) {
	if s.Due() {
		s.Check(w, rng, players, st, spawn)
	}
}

// traderSpawns runs the spawner over rolls days of a ready countdown and
// returns what it spawned and the state after the last.
func traderSpawns(t *testing.T, w TraderWorld, players []SpawnPlayer, rolls int) ([]TraderSpawn, world.TraderSpawnState) {
	t.Helper()
	s := NewWanderingTraderSpawner()
	rng := rand.New(rand.NewSource(1))
	var spawned []TraderSpawn
	st := world.TraderSpawnState{SpawnDelay: traderTickDelay, SpawnChance: traderChanceMax}
	for range rolls {
		for range traderTickDelay {
			tickTrader(s, w, rng, players, &st, func(sp TraderSpawn) { spawned = append(spawned, sp) })
		}
		if st.SpawnDelay != traderSpawnDelay {
			t.Fatalf("delay %d after a roll, want %d", st.SpawnDelay, traderSpawnDelay)
		}
		st.SpawnDelay = traderTickDelay
	}
	return spawned, st
}

func traderGround(t *testing.T) traderTestWorld {
	w := traderTestWorld{spawnTestWorld: newSpawnTestWorld()}
	grass := defaultState(t, "minecraft:grass_block")
	floor(w.testWorld, -60, -60, 60, 60, 63, grass)
	return w
}

// TestTheWanderingTraderSpawnerCountsDownAndRaisesItsChance: the saved delay
// drops 1200 a check, and a day that spawns nothing raises the chance by 25
// up to 75.
// vanilla: WanderingTraderSpawner.tick
func TestTheWanderingTraderSpawnerCountsDownAndRaisesItsChance(t *testing.T) {
	s := NewWanderingTraderSpawner()
	rng := rand.New(rand.NewSource(1))
	// Every roll finds nowhere to stand: the ground is all air.
	w := traderTestWorld{spawnTestWorld: newSpawnTestWorld()}
	players := []SpawnPlayer{{X: 0.5, Y: 64, Z: 0.5}}
	st := world.TraderSpawnState{SpawnDelay: 24000, SpawnChance: 25}
	for range traderTickDelay - 1 {
		tickTrader(s, w, rng, players, &st, func(TraderSpawn) { t.Fatal("spawned over air") })
	}
	if st.SpawnDelay != 24000 {
		t.Fatalf("delay %d before the first check, want 24000", st.SpawnDelay)
	}
	tickTrader(s, w, rng, players, &st, nil)
	if st.SpawnDelay != 22800 {
		t.Fatalf("delay %d after the first check, want 22800", st.SpawnDelay)
	}
	for _, want := range []int32{50, 75, 75} {
		for range 24000 {
			tickTrader(s, w, rng, players, &st, func(TraderSpawn) { t.Fatal("spawned over air") })
		}
		if st.SpawnChance != want {
			t.Fatalf("chance %d, want %d", st.SpawnChance, want)
		}
	}
}

// TestTheWanderingTraderSpawnerStandsATraderNearThePlayer: with no bell the
// trader stands on the ground within 48 blocks of the player and wanders to
// the player's block; a spawn resets the chance to 25.
// vanilla: WanderingTraderSpawner.spawn
func TestTheWanderingTraderSpawnerStandsATraderNearThePlayer(t *testing.T) {
	w := traderGround(t)
	players := []SpawnPlayer{{X: 0.5, Y: 64, Z: 0.5}}
	spawned, _ := traderSpawns(t, w, players, 200)
	if len(spawned) == 0 {
		t.Fatal("no trader in 200 rolls")
	}
	for _, sp := range spawned {
		if sp.Y != 64 || sp.X < -48 || sp.X >= 48 || sp.Z < -48 || sp.Z >= 48 {
			t.Errorf("trader at %d %d %d, want on the ground within 48 of the player", sp.X, sp.Y, sp.Z)
		}
		if sp.Target != (Waypoint{0, 64, 0}) {
			t.Errorf("wander target %v, want the player's block", sp.Target)
		}
	}
	m := NewMob(TypeWanderingTrader, 1, 0.5, 64, 0.5, 1)
	spawned[0].Prepare(m)
	hx, hy, hz, r, ok := m.HomeRestriction()
	if m.DespawnDelay != 48000 || m.WanderTarget == nil || *m.WanderTarget != spawned[0].Target ||
		!ok || r != 16 || (Waypoint{hx, hy, hz}) != spawned[0].Target {
		t.Errorf("prepared delay %d target %v home %d %d %d r%d", m.DespawnDelay, m.WanderTarget, hx, hy, hz, r)
	}

	s := NewWanderingTraderSpawner()
	rng := rand.New(rand.NewSource(3))
	for {
		got := false
		st := world.TraderSpawnState{SpawnDelay: 1, SpawnChance: 75}
		for range traderTickDelay {
			tickTrader(s, w, rng, players, &st, func(TraderSpawn) { got = true })
		}
		if got {
			if st.SpawnChance != traderChanceMin {
				t.Errorf("chance %d after a spawn, want %d", st.SpawnChance, traderChanceMin)
			}
			break
		}
	}
}

// TestTheWanderingTraderSpawnerNeedsALivePlayerAndRoom: no live player
// resets the chance without a spawn, and a 2x3x2 space with a block in it
// refuses the spot.
func TestTheWanderingTraderSpawnerNeedsALivePlayerAndRoom(t *testing.T) {
	w := traderGround(t)
	// A chance of 100 always passes its roll.
	st := world.TraderSpawnState{SpawnDelay: 1, SpawnChance: 100}
	s := NewWanderingTraderSpawner()
	rng := rand.New(rand.NewSource(1))
	for range traderTickDelay {
		tickTrader(s, w, rng, []SpawnPlayer{{X: 0.5, Y: 64, Z: 0.5, Dead: true}}, &st, func(TraderSpawn) {
			t.Fatal("a trader for a dead player")
		})
	}
	if st.SpawnChance != traderChanceMin {
		t.Errorf("chance %d with only a dead player, want %d", st.SpawnChance, traderChanceMin)
	}
	stone := defaultState(t, "minecraft:stone")
	w.set(1, 66, 1, stone)
	if traderHasRoom(w, Waypoint{0, 64, 0}) {
		t.Error("room for a trader under a block two up")
	}
	if !traderHasRoom(w, Waypoint{2, 64, 2}) {
		t.Error("no room on open ground")
	}
}
