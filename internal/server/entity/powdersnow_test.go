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

func TestAnItemSinksIntoPowderSnow(t *testing.T) {
	w := snowField(t)
	it := NewItem(1, protocol.ItemStack{ItemID: 1, Count: 1}, 0.5, 65, 0.5, 0, 0, 0, 0, 1)
	for range 40 {
		it.Step(w, nil)
	}
	if it.Y > 63.5 {
		t.Errorf("an item rests at y=%v, want it sunk into the snow", it.Y)
	}
}

func TestACowWalksIntoPowderSnow(t *testing.T) {
	w := snowField(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 63, 0.5, 1))
	cow.NoAI = true
	cow.OnGround = true
	cow.moveHorizontal(w, cow.collideCtx(), 0.2, 0)
	if cow.X < 0.69 {
		t.Errorf("the cow moved to x=%v, want it carried 0.2 into the snow", cow.X)
	}
}

func TestSandLandsOnPowderSnow(t *testing.T) {
	w := snowField(t)
	f := NewFallingBlock(1, 0.5, 64.5, 0.5, 0, 0, 0, defaultState(t, "minecraft:sand"), 1)
	for i := 0; !f.Dead && i < 1000; i++ {
		f.Step(w)
	}
	if !f.Dead {
		t.Fatal("the sand never landed")
	}
	if f.Y < 63.99 {
		t.Errorf("the sand ended at y=%v, want it held at the top of the snow", f.Y)
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

func TestAFarFallingBodyCollidesWithPowderSnowsTop(t *testing.T) {
	w := snowField(t)
	if got := cellBoxes(w, collideCtx{fallDistance: 3}, defaultState(t, "minecraft:powder_snow"), 0, 63, 0, 70); len(got) != 1 || got[0].MaxY < 0.89 || got[0].MaxY > 0.91 {
		t.Errorf("a body with 3 blocks of fall reads %v, want the 0.9 box", got)
	}
	if got := cellBoxes(w, collideCtx{fallDistance: 2.5}, defaultState(t, "minecraft:powder_snow"), 0, 63, 0, 70); len(got) != 0 {
		t.Errorf("a body with 2.5 blocks of fall reads %v, want nothing", got)
	}
}

// A body that fell more than 2.5 blocks lands on the snow's 0.9 box instead
// of sinking through it.
func TestAnItemAfterALongDropStopsOnPowderSnow(t *testing.T) {
	w := snowField(t)
	it := NewItem(1, protocol.ItemStack{ItemID: 1, Count: 1}, 0.5, 70, 0.5, 0, 0, 0, 0, 1)
	for i := 0; !it.OnGround && i < 80; i++ {
		it.Step(w, nil)
	}
	if !it.OnGround || it.Y < 64.0 {
		t.Errorf("an item that fell 7 blocks landed=%v at y=%v, want it held on the snow's 0.9 top", it.OnGround, it.Y)
	}
}

func TestBankedFallSinksABodyThroughPowderSnowsTop(t *testing.T) {
	w := snowField(t)
	var fall float64
	bankFall(&fall, 70, 66, false)
	if fall != 4 {
		t.Fatalf("bankFall banked %v, want 4", fall)
	}
	bankFall(&fall, 66, 66, true)
	if fall != 0 {
		t.Errorf("bankFall on the ground left %v, want 0", fall)
	}
	if got := cellBoxes(w, fallCtx(3), defaultState(t, "minecraft:powder_snow"), 0, 63, 0, 70); len(got) != 1 {
		t.Errorf("fallCtx(3) reads %v, want the falling box", got)
	}
}

// TNT, a boat and a minecart that fell far bank the fall into their
// collision context and stop on the snow's 0.9 box; they sink in only
// afterwards, once the landing spends the banked fall. The body is read one
// tick after it first passes y=64.
func TestOtherBodiesAfterALongDropStopOnPowderSnow(t *testing.T) {
	bodies := []struct {
		name string
		step func(w testWorld) (stepFn func(), y func() float64)
	}{
		{"tnt", func(w testWorld) (func(), func() float64) {
			b := NewTNT(1, 0.5, 70, 0.5, 0, 0, 0, 1000, 0, 0)
			return func() { b.Step(w) }, func() float64 { return b.Y }
		}},
		{"boat", func(w testWorld) (func(), func() float64) {
			b := NewBoat(1, 0.5, 70, 0.5, 0)
			return func() { b.Step(w, false) }, func() float64 { return b.Y }
		}},
		{"minecart", func(w testWorld) (func(), func() float64) {
			m := NewMinecart(1, 0.5, 70, 0.5)
			return func() { m.Step(w, false) }, func() float64 { return m.Y }
		}},
	}
	for _, c := range bodies {
		t.Run(c.name, func(t *testing.T) {
			step, y := c.step(snowField(t))
			for i := 0; i < 200 && y() >= 64; i++ {
				step()
			}
			step()
			if got := y(); got < 63.8 {
				t.Errorf("%s reached y=%v a tick after the snow's top, want it held on the 0.9 box", c.name, got)
			}
		})
	}
}
