package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// TestTNTBlastIndexNear checks the index yields the bodies within reach in
// list order, plus any added since the build, and the whole list once the
// indexed prefix changed.
func TestTNTBlastIndexNear(t *testing.T) {
	mk := func(x float64) *tntEntity { return &tntEntity{tnt: &entity.TNT{X: x, Y: 64, Z: 0.5}} }
	var list []*tntEntity
	for i := range 60 {
		list = append(list, mk(float64(i)*3+0.5))
	}
	var x tntBlastIndex
	if got := x.near(list, 0, 64, 0, 5); len(got) != len(list) {
		t.Fatalf("inactive index must return the list, got %d", len(got))
	}
	x.build(list)
	got := x.near(list, 90, 64, 0, 7)
	want := []*tntEntity{}
	for _, it := range list {
		if it.tnt.X >= 83 && it.tnt.X <= 97 {
			want = append(want, it)
		}
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("body at %v missing", w.tnt.X)
		}
	}
	if len(got) > len(want)+8 {
		t.Fatalf("index returned %d, want about %d", len(got), len(want))
	}
	if !slices.IsSortedFunc(got, func(a, b *tntEntity) int { return slices.Index(list, a) - slices.Index(list, b) }) {
		t.Fatal("not in list order")
	}
	added := mk(500)
	list = append(list, added)
	if got := x.near(list, 90, 64, 0, 7); got[len(got)-1] != added {
		t.Fatal("body added after the build must be offered")
	}
	if got := x.near(list[1:], 90, 64, 0, 7); len(got) != len(list)-1 {
		t.Fatal("a changed prefix must fall back to the list")
	}
}

func BenchmarkTNTBlastIndex(b *testing.B) {
	const count = 4096
	list := make([]*tntEntity, count)
	for i := range count {
		x := float64(i%32)*0.75 + 0.5
		y := 64 + float64(i/1024)*0.75
		z := float64((i%1024)/32)*0.75 + 0.5
		list[i] = &tntEntity{tnt: &entity.TNT{X: x, Y: y, Z: z}}
	}
	var idx tntBlastIndex
	b.Run("Build", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			idx.build(list)
			idx.drop()
		}
	})
	b.Run("BuildAndNear", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			idx.build(list)
			_ = idx.near(list, 12.5, 65.0, 12.5, 8)
			idx.drop()
		}
	})

	scattered := make([]*tntEntity, count)
	for i := range count {
		x := float64(i%64)*32 + 0.5
		z := float64(i/64)*32 + 0.5
		scattered[i] = &tntEntity{tnt: &entity.TNT{X: x, Y: 64, Z: z}}
	}
	// Place a nearby survivor in an adjacent cell so the sparse query returns
	// candidates across cells without shrinking the scattered bounding box.
	scattered[count-1] = &tntEntity{tnt: &entity.TNT{X: 39.5, Y: 64, Z: 32.5}}
	b.Run("ScatteredBuild", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			idx.build(scattered)
			idx.drop()
		}
	})
	b.Run("ScatteredBuildAndNear", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			idx.build(scattered)
			_ = idx.near(scattered, 32.5, 64.0, 32.5, 8)
			idx.drop()
		}
	})
}
