package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestSparseClipsMatchEveryCellAcrossSections(t *testing.T) {
	w := world.NewDimensionWorld(t.TempDir(), true, world.NewFlatGenerator())
	defer w.Close()
	for _, pos := range []world.ChunkPos{{}, {-1, 0}, {0, -1}, {-1, -1}} {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	names := []string{"air", "water", "stone", "oak_slab", "oak_fence", "bamboo", "pointed_dripstone", "scaffolding", "powder_snow", "lava", "moving_piston", "shulker_box"}
	states := make([]uint16, len(names))
	for i, name := range names {
		states[i] = defaultState(t, "minecraft:"+name)
	}
	rng := rand.New(rand.NewSource(6534))
	for trial := range 500 {
		y := int32(64)
		if trial%2 == 0 {
			y = 80
		}
		for x := int32(-2); x <= 2; x++ {
			for dy := int32(-1); dy <= 2; dy++ {
				for z := int32(-2); z <= 2; z++ {
					w.SetBlock(x, y+dy, z, states[rng.Intn(len(states))])
				}
			}
		}
		box := AABB{-0.3, float64(y), -0.3, 0.3, float64(y) + 1.8, 0.3}
		dx, dy, dz := rng.Float64()*2-1, rng.Float64()*2-1, rng.Float64()*2-1
		// Include the forwarding path through the body-world wrapper.
		view := WithBodies(w, []AABB{{1, float64(y), 1, 2, float64(y) + 1, 2}})
		cells := gatherMoveCells(view, box, dx, dy, dz, 0.6)
		ctx := collideCtx{noBorder: true, moveCells: &cells, walker: trial%2 == 0, lavaWalker: trial%3 == 0, fallingBlock: trial%4 == 0, fallDistance: float64(trial % 5)}
		for axis, d := range []float64{dx, dy, dz} {
			got, top, blocked := clipMove(view, ctx, box, axis, d)
			want, wantTop, wantBlocked := clipMoveEveryCell(view, ctx, box, axis, d)
			if got != want || top != wantTop || blocked != wantBlocked {
				t.Fatalf("trial %d axis %d: sparse = %+v/%v/%v, every cell = %+v/%v/%v", trial, axis, got, top, blocked, want, wantTop, wantBlocked)
			}
		}
		cells.release()
	}
}

type emptyCollisionWorld struct {
	testWorld
	reads int
}

func (w *emptyCollisionWorld) CollisionCellsIn(_, _, _, _, _, _ int32, buf []world.CollisionCell) []world.CollisionCell {
	w.reads++
	return buf[:0]
}

func TestEmptySparseMoveCellsAreCached(t *testing.T) {
	w := &emptyCollisionWorld{testWorld: newTestWorld()}
	box := AABB{0.2, 64, 0.2, 0.8, 65.8, 0.8}
	view := WithBodies(w, []AABB{{100, 64, 100, 101, 65, 101}})
	cells := gatherMoveCells(view, box, 0.1, -0.08, 0.1, 0.6)
	defer cells.release()
	for axis, d := range []float64{0.1, -0.08, 0.1} {
		_, _, blocked := clipMove(view, collideCtx{moveCells: &cells}, box, axis, d)
		if blocked {
			t.Fatalf("empty sweep blocked on axis %d", axis)
		}
	}
	if w.reads != 1 {
		t.Fatalf("empty cell gather read %d times, want 1", w.reads)
	}
}
