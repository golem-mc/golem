package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

type vehicleRainWorld struct {
	testWorld
	queries int
}

func (w *vehicleRainWorld) RainingAt(x, y, z int32) bool {
	w.queries++
	return w.testWorld.RainingAt(x, y, z)
}

func TestVehiclesSkipRainQueriesWithoutFire(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"boat", "minecart"} {
		for _, tc := range []struct {
			name  string
			fire  int
			water bool
		}{
			{name: "dry"},
			{name: "cooldown", fire: -1},
			{name: "water", fire: 100, water: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				w := &vehicleRainWorld{testWorld: newTestWorld()}
				if tc.water {
					w.set(0, 0, 0, boatWater)
				}
				if kind == "boat" {
					b := NewBoat(1, .5, .5, .5, 0)
					b.NoGravity, b.Invulnerable = true, true
					b.FireTicks = tc.fire
					stepBurningBoat(b, w, false)
				} else {
					m := NewMinecart(1, .5, .5, .5)
					m.NoGravity, m.Invulnerable = true, true
					m.FireTicks = tc.fire
					m.Step(w, false)
				}
				if w.queries != 0 {
					t.Fatalf("RainingAt called %d times without fire to extinguish", w.queries)
				}
			})
		}
	}
}

func TestVehicleRainExtinguishesNewIgnition(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"boat", "minecart"} {
		for _, rain := range []bool{false, true} {
			w := newTestWorld()
			w.rain = rain
			w.set(0, 0, 0, registry.MustState("minecraft:fire", nil))
			var fire int
			if kind == "boat" {
				b := NewBoat(1, .5, .5, .5, 0)
				b.NoGravity, b.Invulnerable = true, true
				stepBurningBoat(b, w, false)
				fire = b.FireTicks
			} else {
				m := NewMinecart(1, .5, .5, .5)
				m.NoGravity, m.Invulnerable = true, true
				m.Step(w, false)
				fire = m.FireTicks
			}
			if (fire > 0) == rain {
				t.Errorf("%s (rain %v): FireTicks=%d", kind, rain, fire)
			}
		}
	}
}
