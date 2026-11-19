package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The powder snow's collision is per body: empty for an arrow, an item or a
// plain mob, whole for a falling block or a walker standing above it.
// vanilla: PowderSnowBlock.getCollisionShape

// snowField is a stone floor at y=62 with powder snow filling y=63.
func snowField(t testing.TB) testWorld {
	t.Helper()
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	snow := defaultState(t, "minecraft:powder_snow")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 62, z, stone)
			w.set(x, 63, z, snow)
		}
	}
	return w
}

func TestAnArrowFliesIntoPowderSnow(t *testing.T) {
	w := snowField(t)
	a := NewArrow(1, 0, 63.5, 0, 1, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.Step(w, nil, nil)
	if a.InGround {
		t.Fatal("an arrow stuck in powder snow, want it to fly into the cell")
	}
	if a.X < 0.9 {
		t.Errorf("the arrow moved to x=%v, want its whole step", a.X)
	}
}

func TestALeatherBootedMobStandsOnPowderSnow(t *testing.T) {
	boots, _ := registry.ItemID("minecraft:leather_boots")
	run := func(worn bool) float64 {
		w := snowField(t)
		z := aimless(NewMob(TypeZombie, 1, 0.5, 66, 0.5, 1))
		if worn {
			z.Worn[wornFeet] = protocol.ItemStack{ItemID: boots, Count: 1}
		}
		for range 40 {
			z.Tick(w, nil)
		}
		return z.Y
	}
	if y := run(true); y < 63.99 || y > 64.01 {
		t.Errorf("a zombie in leather boots ended at y=%v, want it on the snow's top", y)
	}
	if y := run(false); y > 63.5 {
		t.Errorf("a bare zombie ended at y=%v, want it sunk through", y)
	}
}
