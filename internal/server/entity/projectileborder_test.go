package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type projectileBorderWorld struct {
	testWorld
	border *world.World
}

func newProjectileBorderWorld() projectileBorderWorld {
	b := world.NewBorder()
	b.SetSize(20)
	w := new(world.World)
	w.SetBorder(b)
	return projectileBorderWorld{testWorld: newTestWorld(), border: w}
}

func (w projectileBorderWorld) InBorderPoint(x, z float64) bool {
	return w.border.InBorderPoint(x, z)
}

func (w projectileBorderWorld) ClampToBorder(x, z float64) (float64, float64) {
	return w.border.ClampToBorder(x, z)
}

func TestProjectileBorderClampsTheEndpoint(t *testing.T) {
	const far = 10 - float64(float32(1e-5))
	for _, tc := range []struct {
		name             string
		x, z, dx, dy, dz float64
		ex, ey, ez       float64
		face             int32
	}{
		{"east", 9, 0, 3, 2, 1, far, 72, 1, protocol.FaceEast},
		{"west", -9, 0, -3, 2, 1, -10, 72, 1, protocol.FaceWest},
		{"south", 0, 9, 1, 2, 3, 1, 72, far, protocol.FaceSouth},
		{"north", 0, -9, 1, 2, -3, 1, 72, -10, protocol.FaceNorth},
		{"corner", 9, 9, 3, 1, 3, far, 71, far, protocol.FaceSouth},
		{"vertical tie", 9, 0, 3, -3, 0, far, 67, 0, protocol.FaceBottom},
		{"included near edge", -10, 0, -1, 0, 0, -10, 70, 0, protocol.FaceWest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newProjectileBorderWorld()
			c := clipProjectileFlight(w, tc.x, 70, tc.z, tc.dx, tc.dy, tc.dz)
			x, y, z := c.end(tc.x, 70, tc.z, tc.dx, tc.dy, tc.dz)
			if !c.ok || !c.border || x != tc.ex || y != tc.ey || z != tc.ez || c.face != tc.face {
				t.Fatalf("clip = %+v, endpoint (%v, %v, %v), want border at (%v, %v, %v), face %d", c, x, y, z, tc.ex, tc.ey, tc.ez, tc.face)
			}
			if c.bx != int32(math.Floor(x)) || c.by != int32(math.Floor(y)) || c.bz != int32(math.Floor(z)) {
				t.Fatalf("hit cell = (%d, %d, %d), want endpoint's cell", c.bx, c.by, c.bz)
			}
			if plain := clipArrowFlight(w, tc.x, 70, tc.z, tc.dx, tc.dy, tc.dz); plain.ok {
				t.Fatalf("plain block clip hit the border: %+v", plain)
			}
		})
	}
}

func TestProjectileBorderLeavesOtherClipsAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		x, dx float64
	}{
		{"inside", 5, 1},
		{"outside enters", 11, -2},
		{"outside exits", 11, 2},
		{"excluded far edge", 10, 2},
		{"stationary near edge", -10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newProjectileBorderWorld()
			c := clipProjectileFlight(w, tc.x, 70, 0, tc.dx, 0, 0)
			if c.ok {
				t.Fatalf("clip = %+v, want a miss", c)
			}
		})
	}
	t.Run("nearer block", func(t *testing.T) {
		w := newProjectileBorderWorld()
		w.set(7, 70, 0, defaultState(t, "minecraft:stone"))
		c := clipProjectileFlight(w, 5, 70.5, 0.5, 10, 0, 0)
		if !c.ok || c.border || c.bx != 7 {
			t.Fatalf("clip = %+v, want the nearer stone", c)
		}
	})
	t.Run("block beyond border", func(t *testing.T) {
		w := newProjectileBorderWorld()
		w.set(12, 70, 0, defaultState(t, "minecraft:stone"))
		c := clipProjectileFlight(w, 9, 70.5, 0.5, 6, 0, 0)
		if !c.ok || !c.border || c.bx != 9 || c.state != 0 {
			t.Fatalf("clip = %+v, want the border's air cell", c)
		}
	})
	t.Run("body world wrapper", func(t *testing.T) {
		w := Bodies{World: newProjectileBorderWorld()}
		if c := clipProjectileFlight(w, 9, 70, 0, 2, 0, 0); !c.border {
			t.Fatalf("wrapped world's border ignored: %+v", c)
		}
	})
}

func TestArrowsAndTridentsBounceWithoutLandingAtTheBorder(t *testing.T) {
	for _, trident := range []bool{false, true} {
		w := newProjectileBorderWorld()
		a := NewArrow(1, 9, 70, 0, 2, 0.5, 0.25, true, ArrowBaseDamage, PickupAllowed, 0, 3)
		a.Trident = trident
		a.PierceLevel, a.HitSound = 2, registry.SoundItemCrossbowHit
		events := a.Step(w, nil, nil)
		if a.Dead || a.InGround || a.X >= 10 || a.VX >= 0 || a.VY >= 0 || a.VZ >= 0 {
			t.Fatalf("trident %v: arrow after bounce = %+v", trident, a)
		}
		if !closeEnough(a.VX, -0.2*arrowAirDrag) || !a.Crit || a.PierceLevel != 2 || a.HitSound != registry.SoundItemCrossbowHit {
			t.Fatalf("trident %v: bounce changed flight properties: %+v", trident, a)
		}
		if len(events) != 1 || events[0].Kind != ArrowDeflected || events[0].Target.EntityID != 0 {
			t.Fatalf("trident %v: events = %+v, want a deflection without a body", trident, events)
		}
	}
}

func TestAFishingHookBouncesOffTheBorder(t *testing.T) {
	w := newProjectileBorderWorld()
	b := NewBobber(1, 9, 70, 0, -90, 0, 7, 3, 0, 0)
	b.X, b.Y, b.Z = 9, 70, 0
	b.VX, b.VY, b.VZ = 2, 0, 0
	owner := &BobberOwner{Body: PlayerTarget(7, 5, 70, 0, false), Fishing: true}
	events := b.Step(w, owner, nil)
	if b.Dead || b.X >= 9 || b.VX >= 0 || b.State != BobberFlying || b.HookedID != 0 {
		t.Fatalf("bobber after bounce = %+v", b)
	}
	for _, ev := range events {
		if ev.Kind == BobberHitBlock || ev.Kind == BobberHitTarget {
			t.Fatalf("border bounce reported a landing: %+v", ev)
		}
	}
}

func TestOtherProjectilesLandOnTheBorder(t *testing.T) {
	for kind := ThrownEgg; kind <= ThrownLlamaSpit; kind++ {
		t.Run(registry.EntityTypes[kind.EntityType()], func(t *testing.T) {
			w := newProjectileBorderWorld()
			p := NewThrown(1, kind, 9, 70, 0, 2, 0, 0, 0, 3)
			events := p.Step(w, nil, nil)
			if !p.Dead {
				t.Fatalf("%v flew through the border: %+v", kind, p)
			}
			for _, ev := range events {
				if ev.Kind == ThrownBreak && ev.BlockHit.Border && ev.X < 10 && ev.X >= 9 {
					return
				}
			}
			t.Fatalf("%v events = %+v, want a break at the border", kind, events)
		})
	}
	for _, stars := range []int32{0, 1} {
		w := newProjectileBorderWorld()
		f := NewFirework(1, 9, 70, 0, 0, 0, stars, 0, 3)
		f.VX = 2
		events := f.Step(w, nil, nil, nil)
		if f.Dead != (stars > 0) || f.X >= 10 {
			t.Fatalf("stars %d: firework after border hit = %+v", stars, f)
		}
		if len(events) == 0 || events[0].Kind != FireworkHitBlock || !events[0].BlockHit.Border {
			t.Fatalf("stars %d: events = %+v, want border hit", stars, events)
		}
	}
}
