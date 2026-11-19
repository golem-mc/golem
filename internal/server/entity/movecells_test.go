package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

type moveCellsWorld struct {
	testWorld
	reads        int
	pistonReads  int
	shulkerReads int
}

func (w *moveCellsWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	w.reads++
	return w.testWorld.BlocksIn(x0, y0, z0, x1, y1, z1, buf)
}

func (w *moveCellsWorld) MovingPistonBoxes(x, y, z int32) []world.CollisionBox {
	w.pistonReads++
	return []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}
}

func (w *moveCellsWorld) ShulkerBoxBoxes(state uint16, x, y, z int32) []world.CollisionBox {
	w.shulkerReads++
	return []world.CollisionBox{{MaxX: 1, MaxY: 1.5, MaxZ: 1}}
}

func TestMoveCellsCoverMovesAndStepRetry(t *testing.T) {
	for _, kind := range []string{"mob", "item", "box", "step", "vertical", "still"} {
		t.Run(kind, func(t *testing.T) {
			w := &moveCellsWorld{testWorld: newTestWorld()}
			floor(w.testWorld, -3, -3, 3, 3, 63, defaultState(t, "minecraft:stone"))
			switch kind {
			case "vertical", "still":
				m := NewMob(TypeCow, 1, 0.5, 65, 0.5, 1)
				dy := -0.3
				if kind == "still" {
					dy = 0
				}
				m.move(w, 0, dy, 0)
			case "mob":
				m := NewMob(TypeCow, 1, 0.5, 65, 0.5, 1)
				m.move(w, 0.2, -0.3, -0.1)
			case "item":
				it := &Item{X: 0.5, Y: 65, Z: 0.5, VX: 0.2, VY: -0.3, VZ: -0.1}
				it.move(w)
			case "box":
				x, y, z, vx, vy, vz := 0.5, 65.0, 0.5, 0.2, -0.3, -0.1
				stuck := stuckFactor{}
				moveBox(w, noCtx, &x, &y, &z, &vx, &vy, &vz, &stuck, 0.3, 0.6)
			case "step":
				w.set(1, 64, 0, defaultState(t, "minecraft:oak_slab"))
				m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
				m.OnGround = true
				m.move(w, 0.6, -0.1, 0.1)
				if m.Y != 64.5 || math.Abs(m.X-1.1) > 1e-12 {
					t.Fatalf("step ended at %v,%v,%v", m.X, m.Y, m.Z)
				}
			}
			want := 1
			if kind == "still" {
				want = 0
			}
			if kind == "step" {
				// boxFits keeps its separate overlap query.
				want++
			}
			if w.reads != want {
				t.Fatalf("BlocksIn calls = %d, want %d", w.reads, want)
			}
		})
	}
}
