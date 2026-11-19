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

func TestVillageSiegeGatesAndFailedSetup(t *testing.T) {
	for _, gate := range []string{"day", "enemies", "spectator", "village", "biome", "light", "floor"} {
		t.Run(gate, func(t *testing.T) {
			w := newSiegeWorld()
			p := SpawnPlayer{Y: 64}
			enemies := true
			switch gate {
			case "day":
				w.bright = true
			case "enemies":
				enemies = false
			case "spectator":
				p.Spectator = true
			case "village":
				w.village = false
			case "biome":
				w.biomeName = "minecraft:mushroom_fields"
			case "light":
				w.blockLights = map[[3]int32]uint8{}
				w.sky = 15
				w.darken = 0
			case "floor":
				w.noFloor = true
			}
			var siege VillageSiege
			siege.ForceTonight()
			rng := rand.New(rand.NewSource(1))
			spawn := func(SpawnRequest) { t.Fatal("spawn through gate") }
			for range 60 {
				siege.Tick(w, rng, enemies, []SpawnPlayer{p}, spawn)
			}
			if gate == "light" && (!siege.setup || siege.tonight) {
				t.Fatal("failed setup should consume the night")
			}
		})
	}
}
