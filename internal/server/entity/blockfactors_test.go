package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestASoulSandOrHoneyFloorKeepsFourTenthsOfAMobsDrift holds the speed
// factor to the block under the feet: a shove of 0.2 leaves 0.4 times what
// it leaves on stone after one tick.
// vanilla: Entity.getBlockSpeedFactor
func TestASoulSandOrHoneyFloorKeepsFourTenthsOfAMobsDrift(t *testing.T) {
	drift := func(block string) float64 {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, defaultState(t, block))
		cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
		for range 4 {
			cow.Tick(w, nil)
		}
		cow.VX = 0.2
		cow.Tick(w, nil)
		return cow.VX
	}
	stone := drift("minecraft:stone")
	for _, block := range []string{"minecraft:soul_sand", "minecraft:honey_block"} {
		if got := drift(block) / stone; math.Abs(got-0.4) > 0.01 {
			t.Errorf("drift on %s is %v of stone's, want 0.4", block, got)
		}
	}
}

// TestACobwebSlowsAMobAndAnItem holds the stuck multiplier: a body moving
// through a cobweb travels a quarter of the way it travels in the air, and a
// spider or the wither is not slowed.
// vanilla: WebBlock.entityInside, Spider.makeStuckInBlock, WitherBoss.makeStuckInBlock
func TestACobwebSlowsAMobAndAnItem(t *testing.T) {
	web := defaultState(t, "minecraft:cobweb")
	run := func(typ MobType, block uint16) float64 {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, defaultState(t, "minecraft:stone"))
		w.set(0, 64, 0, block)
		w.set(1, 64, 0, block)
		m := aimless(NewMob(typ, 1, 0.5, 64, 0.5, 1))
		m.Tick(w, nil)
		m.Tick(w, nil)
		was := m.X
		m.VX = 0.1
		m.Tick(w, nil)
		m.VX = 0.1
		m.Tick(w, nil)
		return m.X - was
	}
	if free, webbed := run(TypeCow, 0), run(TypeCow, web); webbed > free*0.4 {
		t.Errorf("cow moved %v in a cobweb against %v free, want far less", webbed, free)
	}
	if free, webbed := run(TypeSpider, 0), run(TypeSpider, web); webbed < free*0.9 {
		t.Errorf("spider moved %v in a cobweb against %v free, want no slowing", webbed, free)
	}
	if free, webbed := run(TypeWither, 0), run(TypeWither, web); free <= 0 || webbed != free {
		t.Errorf("wither moved %v in a cobweb against %v free, want the same", webbed, free)
	}

	fall := func(block uint16) float64 {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 50, defaultState(t, "minecraft:stone"))
		for y := int32(56); y <= 60; y++ {
			w.set(0, y, 0, block)
		}
		it := NewItem(1, protocol.ItemStack{ItemID: 1, Count: 1}, 0.5, 60.5, 0.5, 0, 0, 0, 0, 1)
		for range 8 {
			it.Step(w, nil)
		}
		return 60.5 - it.Y
	}
	if free, webbed := fall(0), fall(web); webbed > free*0.3 {
		t.Errorf("item fell %v in a cobweb against %v free, want far less", webbed, free)
	}
}
