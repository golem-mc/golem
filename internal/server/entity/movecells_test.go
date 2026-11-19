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

func TestMoveCellsFallbackAndDynamicShapes(t *testing.T) {
	w := &moveCellsWorld{testWorld: newTestWorld()}
	w.set(1, 64, 0, defaultState(t, "minecraft:moving_piston"))
	w.set(0, 64, 1, defaultState(t, "minecraft:shulker_box"))
	b := AABB{0.2, 64, 0.2, 0.8, 65.8, 0.8}
	cells := gatherMoveCells(w, b, 0.6, -0.2, 0.6, 0.6)
	defer cells.release()
	ctx := collideCtx{moveCells: &cells}
	for i, axis := range []int{axisX, axisZ} {
		got, top, blocked := clipMove(w, ctx, b, axis, 0.6)
		reads := w.reads
		want, wantTop, wantBlocked := clipMove(w, noCtx, b, axis, 0.6)
		if reads != i+1 { // one prefetch, plus the preceding uncached comparison
			t.Fatalf("cached clip read BlocksIn: %d calls", reads)
		}
		if got != want || top != wantTop || blocked != wantBlocked {
			t.Fatalf("axis %d: cached clip differs", axis)
		}
	}
	if w.pistonReads != 4 || w.shulkerReads != 4 {
		t.Fatalf("shape reads: piston %d, shulker %d", w.pistonReads, w.shulkerReads)
	}
	// A sweep outside the window must fetch its own full range, not truncate.
	w.set(-5, 64, 0, defaultState(t, "minecraft:stone"))
	reads := w.reads
	got, top, blocked := clipMove(w, ctx, b, axisX, -6)
	if w.reads != reads+1 {
		t.Fatalf("outside clip did not fall back")
	}
	want, wantTop, wantBlocked := clipMove(w, noCtx, b, axisX, -6)
	if got != want || top != wantTop || blocked != wantBlocked {
		t.Fatal("fallback clip differs")
	}
}
