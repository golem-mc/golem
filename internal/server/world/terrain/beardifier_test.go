package terrain

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The beardifier's volume path and point path agree.
func TestBeardifierVolumeMatchesPoints(t *testing.T) {
	b := NewBeardifier(
		[]levelgen.BeardRigid{
			{Box: levelgen.BoundingBox{MinX: 4, MinY: 60, MinZ: 4, MaxX: 20, MaxY: 70, MaxZ: 20}, Adjustment: levelgen.AdjustBeardThin},
			{Box: levelgen.BoundingBox{MinX: 30, MinY: 50, MinZ: 0, MaxX: 40, MaxY: 58, MaxZ: 10}, Adjustment: levelgen.AdjustBury},
			{Box: levelgen.BoundingBox{MinX: 0, MinY: 40, MinZ: 30, MaxX: 8, MaxY: 48, MaxZ: 38}, Adjustment: levelgen.AdjustEncapsulate},
		},
		[]levelgen.BeardJunction{{X: 12, GroundY: 64, Z: 24}},
	)
	c := NewContext(b)
	v := NewVolume(16, 40, 16, 0, 40, 0)
	out := make([]float32, v.Size())
	BeardifierSampler().SampleVolume(c, out, v)
	nonZero := 0
	for z := range v.SizeZ {
		for x := range v.SizeX {
			for y := range v.SizeY {
				want := BeardifierSampler().Sample(c, v.BlockX(x), v.BlockY(y), v.BlockZ(z))
				if got := out[v.Index(x, y, z)]; got != want {
					t.Fatalf("(%d,%d,%d): volume %v, point %v", x, y, z, got, want)
				}
				if want != 0 {
					nonZero++
				}
			}
		}
	}
	if nonZero == 0 {
		t.Fatal("beardifier added nothing")
	}
}
