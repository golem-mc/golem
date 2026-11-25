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

// BenchmarkClipMoveMixedHulls clips a walking box against a tall far hull and
// a low near one, in both orders, so the obstacle top resets on each.
func BenchmarkClipMoveMixedHulls(b *testing.B) {
	pig := AABB{0, 63.6, 0, 0.6, 64.4, 0.6}
	near := AABB{0.7, 63, 0, 1.7, 64, 1}
	far := AABB{1.1, 63, 0, 2.1, 66, 1}
	for _, tc := range []struct {
		name  string
		hulls []AABB
	}{
		{"far-first", []AABB{far, near}},
		{"near-first", []AABB{near, far}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := hullWorld{testWorld: newTestWorld(), hulls: tc.hulls}
			b.ReportAllocs()
			for b.Loop() {
				clipMove(w, collideCtx{}, pig, axisX, 1)
			}
		})
	}
}
