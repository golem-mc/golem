package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func stateOf(t *testing.T, name, props string) uint16 {
	t.Helper()
	for _, s := range registry.AllStates[name] {
		if s.Props == props {
			return s.ID
		}
	}
	t.Fatalf("no %s[%s]", name, props)
	return 0
}

func TestStridersSpawnOnlyInLava(t *testing.T) {
	w := newSpawnTestWorld()
	w.biomeName = "minecraft:nether_wastes"
	w.dimension = "minecraft:the_nether"
	const y = 31
	bx, bz := atChunk(0, 0)
	netherrack := defaultState(t, "minecraft:netherrack")
	lava := defaultState(t, "minecraft:lava")
	for dx := int32(-12); dx <= 12; dx++ {
		for dz := int32(-12); dz <= 12; dz++ {
			w.set(bx+dx, y-1, bz+dz, netherrack)
			w.set(bx+dx, y, bz+dz, lava)
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 40.5, Y: float64(y), Z: float64(bz) + 0.5}}
	striders := 0
	for seed := int64(1); seed <= 300; seed++ {
		var bodies []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, y, bx, bz,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryCreature, func(r SpawnRequest) {
				if r.Type != TypeStrider {
					return
				}
				striders++
				if !rules.IsLava(w.BlockAt(int32(math.Floor(r.X)), int32(r.Y), int32(math.Floor(r.Z)))) {
					t.Errorf("strider requested at (%v, %v, %v), outside lava", r.X, r.Y, r.Z)
				}
			})
	}
	if striders == 0 {
		t.Error("no strider requested over a lava lake in the nether wastes")
	}
	if striderSpawnOk(w, bx, y-1, bz) || striderSpawnOk(w, bx, y+1, bz) {
		t.Error("a strider may spawn in netherrack or in the air over the lake")
	}
	w.set(bx, y+1, bz, netherrack)
	if striderSpawnOk(w, bx, y, bz) {
		t.Error("a strider may spawn in lava roofed by a solid block")
	}
}

func TestAStriderPathsOverALavaLake(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	floor(w, -10, -10, 20, 10, 63, stone)
	for x := int32(4); x <= 12; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, lava)
			w.set(x, 62, z, lava)
		}
	}
	m := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
	if !m.seekTo(w, 8.5, 63, 0.5, 1, navReach) {
		t.Fatal("no path to the middle of the lava lake")
	}
	path := m.nav.path
	if last := path[len(path)-1]; last.X < 7 || last.Y != 63 {
		t.Errorf("the path ends at %v, want a lava cell from x=7 at y=63", last)
	}
	// A grown mob that cannot stand on lava finds no way across.
	h := NewMob(TypeHoglin, 2, 0.5, 64, 0.5, 1)
	if h.seekTo(w, 8.5, 63, 0.5, 1, navReach) && len(h.nav.path) > 0 && h.nav.path[len(h.nav.path)-1].X >= 8 {
		t.Error("a hoglin found a way onto the lava")
	}
}

func TestAStriderTakesOneDamageFromWaterOrRain(t *testing.T) {
	for _, tc := range []struct {
		name              string
		water, rain, roof bool
		damage            float64
	}{
		{"water", true, false, false, 1}, {"rain", false, true, false, 1}, {"roof", false, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.rain = tc.rain
			if tc.water {
				w.set(0, 64, 0, defaultState(t, "minecraft:water"))
			}
			if tc.roof {
				w.set(0, 67, 0, defaultState(t, "minecraft:stone"))
			}
			m := NewMob(TypeStrider, 1, 0.5, 64, 0.5, 1)
			m.NoAI = true
			m.Tick(w, nil)
			if got := m.MaxHealth - m.Health; got != tc.damage {
				t.Errorf("damage=%g, want %g", got, tc.damage)
			}
		})
	}
}
