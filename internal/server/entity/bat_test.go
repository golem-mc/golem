package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
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

// TestABatWakesWhenItsCeilingGoesAndWhenHurt checks the other two wakes.
// vanilla: Bat.customServerAiStep, Bat.hurtServer
func TestABatWakesWhenItsCeilingGoesAndWhenHurt(t *testing.T) {
	m, w := hangingBat(t)
	w.set(0, 31, 0, 0)
	m.Tick(w, nil)
	if m.Resting {
		t.Error("a bat stayed resting under no ceiling")
	}

	h, w2 := hangingBat(t)
	h.HurtByWorld(1, rules.DamageTypeCramming)
	if h.Resting {
		t.Error("a hurt bat stayed resting")
	}
	h.Tick(w2, nil)
}

// TestAFlyingBatStaysAloftAndMoves checks the flight: no gravity fall to the
// floor in a hundred ticks, and a different place from the start.
// vanilla: Bat.customServerAiStep
func TestAFlyingBatStaysAloftAndMoves(t *testing.T) {
	m, w := hangingBat(t)
	for x := int32(-40); x <= 40; x++ {
		for z := int32(-40); z <= 40; z++ {
			w.set(x, 31, z, 0)
			w.set(x, 33, z, 0)
			w.set(x, 34, z, 0)
		}
	}
	m.Resting = false
	startX, startZ := m.X, m.Z
	for range 200 {
		m.Tick(w, nil)
		if m.Y <= 29.9 {
			t.Fatalf("the bat sank to the floor at y=%v", m.Y)
		}
	}
	if m.X == startX && m.Z == startZ {
		t.Error("the flying bat never left its place")
	}
}

// TestABatIsAmbientNotACreatureOrMonster checks the category predicates the
// caps count by, and the vanilla box and health.
func TestABatIsAmbientNotACreatureOrMonster(t *testing.T) {
	if !AmbientKind(TypeBat) || MonsterKind(TypeBat) || CreatureKind(TypeBat) {
		t.Errorf("bat ambient=%v monster=%v creature=%v", AmbientKind(TypeBat), MonsterKind(TypeBat), CreatureKind(TypeBat))
	}
	if AmbientKind(TypeCow) || AmbientKind(TypeZombie) {
		t.Error("a cow or zombie counted as ambient")
	}
	if w, h := KindDims(TypeBat); w != 0.5 || h != 0.9 {
		t.Errorf("box %v by %v, want 0.5 by 0.9", w, h)
	}
	if m := NewMob(TypeBat, 1, 0, 0, 0, 1); m.MaxHealth != 6 {
		t.Errorf("health %v, want 6", m.MaxHealth)
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

// TestBatsStopAtTheAmbientCap checks the category's own cap of fifteen per
// chunk-equivalent: a full global count stands nothing up, and monsters
// do not count toward it.
// vanilla: MobCategory.AMBIENT
func TestBatsStopAtTheAmbientCap(t *testing.T) {
	w := caveWorld(t)
	bx, bz := atChunk(0, 0)
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 100.5, Y: 30, Z: float64(bz) + 0.5}}
	chunks := []ChunkPos{{}}
	run := func(global int) int {
		n := 0
		for seed := int64(1); seed < 400; seed++ {
			counts := &SpawnCounts{Global: global, PerPlayer: map[int32]int{}}
			SpawnNaturally(w, rand.New(rand.NewSource(seed)), chunks, players, nil, nil, []SpawnPass{AmbientPass(counts)}, 289,
				func(SpawnRequest) { n++ })
		}
		return n
	}
	if run(0) == 0 {
		t.Fatal("control: an empty count stood up no bat")
	}
	if got := run(15); got != 0 {
		t.Errorf("a global count at 15 of 15 stood up %d bats", got)
	}
}

// TestABatDoesNotWakeForAPlayerBehindAWall checks that the wake needs line of
// sight: a wall between the bat and a player three blocks away leaves it
// asleep.
// vanilla: Bat.BAT_RESTING_TARGETING, TargetingConditions.checkLineOfSight
func TestABatDoesNotWakeForAPlayerBehindAWall(t *testing.T) {
	m, w := hangingBat(t)
	for x := int32(-2); x <= 2; x++ {
		for y := int32(30); y <= 32; y++ {
			w.groundAt(t, x, y, 2)
		}
	}
	m.Tick(w, []Player{{X: 0.5, Y: 30, Z: 3.5, EntityID: 2}})
	if !m.Resting {
		t.Fatal("a bat woke for a player behind a wall")
	}
}

// TestAFlyingBatGetsNoForwardPush checks that flight adds no walk speed:
// MoveControl resets the forward input Bat.customServerAiStep sets.
// vanilla: Bat.customServerAiStep, MoveControl.tick
func TestAFlyingBatGetsNoForwardPush(t *testing.T) {
	m, w := hangingBat(t)
	m.Resting = false
	for range 20 {
		m.Tick(w, nil)
		if m.walk.speed != 0 {
			t.Fatalf("a flying bat walks at speed %v, want 0", m.walk.speed)
		}
	}
}
