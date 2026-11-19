package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkClipMoveFlatGround includes the cell gather shared by a walking
// mob's vertical and horizontal clips, including its step-up window.
func BenchmarkClipMoveFlatGround(b *testing.B) {
	w := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer w.Close()
	for _, pos := range []world.ChunkPos{{}, {-1, 0}, {0, -1}, {-1, -1}} {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	for _, tc := range []struct {
		name string
		x, z float64
	}{
		{"section-centre", 8.5, 8.5},
		{"section-corner", 0.5, 0.5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			box := AABB{tc.x - 0.45, 64, tc.z - 0.45, tc.x + 0.45, 65.4, tc.z + 0.45}
			b.ReportAllocs()
			for b.Loop() {
				cells := gatherMoveCells(w, box, 0.1, -0.08, 0.07, 0.6)
				ctx := collideCtx{noBorder: true, moveCells: &cells}
				moved, _, _ := clipMove(w, ctx, box, axisY, -0.08)
				moved, _, _ = clipMove(w, ctx, moved, axisX, 0.1)
				clipMove(w, ctx, moved, axisZ, 0.07)
				cells.release()
			}
		})
	}
}
