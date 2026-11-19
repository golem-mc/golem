package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type siegeTestWorld struct {
	spawnTestWorld
	village, marker, noFloor bool
}

func (w siegeTestWorld) IsVillage(x, y, z int32) bool { return w.village }
func (w siegeTestWorld) VillageSiegeTime() bool       { return w.marker }
func (w siegeTestWorld) BlockAt(x, y, z int32) uint16 {
	if y == w.surface && !w.noFloor {
		return registry.BlockStone.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
func newSiegeWorld() siegeTestWorld {
	w := siegeTestWorld{spawnTestWorld: newSpawnTestWorld(), village: true}
	w.surface = 63
	return w
}

func TestVillageSiegeRollAndCadence(t *testing.T) {
	rolled := 0
	for seed := range int64(100) {
		w := newSiegeWorld()
		w.marker = true
		want := rand.New(rand.NewSource(seed)).Intn(10) == 0
		if want {
			rolled++
		}
		var siege VillageSiege
		count := 0
		spawn := func(r SpawnRequest) {
			count++
			if r.Type != TypeZombie || r.Y != 64 || r.Yaw < 0 || r.Yaw >= 360 {
				t.Fatalf("spawn: %+v", r)
			}
		}
		rng := rand.New(rand.NewSource(seed))
		siege.Tick(w, rng, true, []SpawnPlayer{{Y: 64}}, spawn)
		w.marker = false
		for tick := 2; tick <= 100; tick++ {
			siege.Tick(w, rng, true, []SpawnPlayer{{Y: 64}}, spawn)
			expected := 0
			if want {
				expected = min(20, (tick+2)/3)
			}
			if count != expected {
				t.Fatalf("seed %d tick %d: got %d, want %d", seed, tick, count, expected)
			}
		}
	}
	if rolled == 0 || rolled == 100 {
		t.Fatalf("rolls: %d", rolled)
	}
}
