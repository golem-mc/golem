package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestNetherPortalPositionMapsOffsetsAcrossDifferentOpenings(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		from, to := testWorld{}, testWorld{}
		for x := range int32(4) {
			for y := int32(64); y < 67; y++ {
				from.set(x, y, 0, NetherPortalX)
			}
		}
		exit := world.SpawnPos{X: 10, Y: 80, Z: 20}
		for i := range int32(2) {
			for y := int32(80); y < 83; y++ {
				if rotate {
					to.set(10, y, 20+i, NetherPortalZ)
				} else {
					to.set(10+i, y, 20, NetherPortalX)
				}
			}
		}
		for _, centered := range []bool{false, true} {
			got := NetherPortalPosition(from, to, world.SpawnPos{X: 2, Y: 65}, exit, [3]float64{2.5, 65.25, 0.2}, 1.4, 1.6, centered)
			along := 0.7 + 0.6*((2.5-0.7)/2.6)
			across := 20.2
			if centered {
				across = 20.5
			}
			want := [3]float64{10 + along, 81.25, across}
			if rotate {
				want = [3]float64{across - 10, 81.25, 20 + along}
			}
			for i := range got {
				if math.Abs(got[i]-want[i]) > 1e-12 {
					t.Errorf("rotated %v, centered %v: position = %v, want %v", rotate, centered, got, want)
					break
				}
			}
		}
	}
}

func TestPortalRectangleChoosesTheLargestOpening(t *testing.T) {
	w := testWorld{}
	for x := range int32(3) {
		for y := int32(64); y < 67; y++ {
			w.set(x, y, 0, NetherPortalX)
		}
	}
	w.set(3, 65, 0, NetherPortalX)
	got := portalRectangleAt(w, world.SpawnPos{X: 1, Y: 65})
	if got.min != (world.SpawnPos{Y: 64}) || got.width != 3 || got.height != 3 {
		t.Fatalf("rectangle = %+v, want (0,64,0), 3 by 3", got)
	}
}
