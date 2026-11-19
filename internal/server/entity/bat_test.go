package entity

import (
	"math/rand"
	"testing"
)

// batWorld is a spawnTestWorld whose air is the empty state, as the bat's
// flight target reads it.
type batWorld struct{ spawnTestWorld }

func (w batWorld) IsAirAt(x, y, z int32) bool { return w.BlockAt(x, y, z) == 0 }

// caveWorld is a dark cave: stone floor at y=29, stone roof at y=33 over a
// wide area, the surface far above.
func caveWorld(t *testing.T) batWorld {
	w := batWorld{newSpawnTestWorld()}
	w.sky, w.surface = 0, 60
	bx, bz := atChunk(0, 0)
	for dx := int32(-40); dx <= 40; dx++ {
		for dz := int32(-40); dz <= 40; dz++ {
			w.groundAt(t, bx+dx, 29, bz+dz)
			w.groundAt(t, bx+dx, 33, bz+dz)
		}
	}
	return w
}

func hangingBat(t *testing.T) (*Mob, batWorld) {
	w := caveWorld(t)
	w.groundAt(t, 0, 31, 0)
	m := NewMob(TypeBat, 1, 0.5, 30.1, 0.5, 1)
	return m, w
}

// TestABatHangsUnderACeiling checks that a fresh bat rests with its box's
// top against the block above, and stays put and still.
// vanilla: Bat.tick, Bat.customServerAiStep
func TestABatHangsUnderACeiling(t *testing.T) {
	m, w := hangingBat(t)
	if !m.Resting {
		t.Fatal("a fresh bat is not resting")
	}
	for range 100 {
		m.Tick(w, nil)
	}
	if !m.Resting {
		t.Fatal("a bat under a solid ceiling with nobody near woke")
	}
	if want := 31 - 0.9; m.Y < want-1e-9 || m.Y > want+1e-9 || m.VY != 0 {
		t.Errorf("hanging at y=%v vy=%v, want y=%v at rest", m.Y, m.VY, want)
	}
}

// TestABatWakesWhenAPlayerIsWithinFourBlocks checks the wake range and the
// takeoff event, and that a spectator or a player farther off leaves it be.
// vanilla: Bat.BAT_RESTING_TARGETING
func TestABatWakesWhenAPlayerIsWithinFourBlocks(t *testing.T) {
	m, w := hangingBat(t)
	m.Tick(w, []Player{{X: 0.5, Y: 30, Z: 4.5, EntityID: 2}})
	if !m.Resting {
		t.Fatal("a bat woke for a player past four blocks")
	}
	m.Tick(w, []Player{{X: 0.5, Y: 30, Z: 3.5, EntityID: 2, Spectator: true}})
	if !m.Resting {
		t.Fatal("a bat woke for a spectator")
	}
	events := m.Tick(w, []Player{{X: 0.5, Y: 30, Z: 3.5, EntityID: 2}})
	if m.Resting {
		t.Fatal("a bat stayed asleep with a player three blocks away")
	}
	took := false
	for _, e := range events {
		took = took || e.Kind == MobBatTookOff
	}
	if !took {
		t.Error("the wake told no takeoff")
	}
}

// batsFromPasses runs the position pass over many seeds in the world and
// counts the requests made, with the types seen.
func batsFromPasses(w batWorld, counts func() *SpawnCounts) (n int, types map[MobType]bool) {
	types = map[MobType]bool{}
	bx, bz := atChunk(0, 0)
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 100.5, Y: 30, Z: float64(bz) + 0.5}}
	for seed := int64(1); seed < 300; seed++ {
		var bodies []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 30, bx, bz,
			players, &bodies, &SpawnPotential{}, counts(), CategoryAmbient, func(r SpawnRequest) { n++; types[r.Type] = true })
	}
	return n, types
}

// TestBatsSpawnInDarkCavesBelowTheSurface checks Bat.checkBatSpawnRules: a
// dark cave under the surface fills with bats and nothing else, while a lit
// one, one open to the sky and one floored by a non-spawnable block do not.
// vanilla: Bat.checkBatSpawnRules
func TestBatsSpawnInDarkCavesBelowTheSurface(t *testing.T) {
	fresh := func() *SpawnCounts { return &SpawnCounts{PerPlayer: map[int32]int{}} }
	w := caveWorld(t)
	n, types := batsFromPasses(w, fresh)
	if n == 0 || len(types) != 1 || !types[TypeBat] {
		t.Fatalf("dark cave: %d requests of %v, want bats only", n, types)
	}

	lit := caveWorld(t)
	lit.block = 15
	if n, _ := batsFromPasses(lit, fresh); n != 0 {
		t.Errorf("a lit cave spawned %d bats", n)
	}

	open := caveWorld(t)
	open.surface = 29
	if n, _ := batsFromPasses(open, fresh); n != 0 {
		t.Errorf("a cell at the surface spawned %d bats", n)
	}

	dirt := caveWorld(t)
	bx, bz := atChunk(0, 0)
	for dx := int32(-40); dx <= 40; dx++ {
		for dz := int32(-40); dz <= 40; dz++ {
			dirt.set(bx+dx, 29, bz+dz, defaultState(t, "minecraft:cobblestone"))
		}
	}
	if n, _ := batsFromPasses(dirt, fresh); n != 0 {
		t.Errorf("cobblestone floor spawned %d bats", n)
	}
}
