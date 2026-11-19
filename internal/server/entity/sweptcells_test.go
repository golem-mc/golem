package entity

import (
	"maps"
	"testing"
)

// A box moving 3 blocks along -x yields each cell between its start and end
// once, the start's at step 0 and the steps never going back.
func TestForEachCellBetweenSweepsThePathOnceInStepOrder(t *testing.T) {
	box := AABB{4.75, 65.2, 0.25, 5.25, 65.7, 0.75}
	box.deflate(insideShave)
	seen := map[int32]int{}
	last := -1
	forEachCellBetween(8, 65.2, 0.5, 5, 65.2, 0.5, box, func(x, y, z int32, step int) bool {
		if y != 65 || z != 0 {
			t.Errorf("cell (%d,%d,%d) is off the path", x, y, z)
		}
		if _, dup := seen[x]; dup {
			t.Errorf("cell x=%d yielded twice", x)
		}
		if step < last {
			t.Errorf("cell x=%d at step %d after step %d", x, step, last)
		}
		seen[x], last = step, step
		return true
	})
	for x := int32(4); x <= 8; x++ {
		if _, ok := seen[x]; !ok {
			t.Errorf("cell x=%d was not yielded", x)
		}
	}
	if seen[7] != 0 || seen[8] != 0 || seen[6] == 0 {
		t.Errorf("steps = %v, want the start cells 7 and 8 at step 0 and the crossed cell 6 later", seen)
	}
}

// The cells a box sweeps, with their steps, are the ones vanilla's walk
// visits, including the cells its perpendicular corner skips on a diagonal.
// The wants were printed by BlockGetter.forEachBlockIntersectedBetween on
// the 26.3 server's classes, for these boxes (at the target) and moves.
func TestSweptCellsMatchVanilla(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		box        AABB
		dx, dy, dz float64
		want       map[[3]int32]int
	}{
		{
			"along x", AABB{10.401229870736593, 7.725374616476276, 4.451788826072039, 10.50221141153095, 9.374601938667109, 4.552770366866398},
			1.8274573152903955, 0, 0,
			map[[3]int32]int{
				{8, 7, 4}: 0, {8, 8, 4}: 0, {8, 9, 4}: 0, {9, 7, 4}: 1, {9, 8, 4}: 1, {9, 9, 4}: 1,
				{10, 7, 4}: 2, {10, 8, 4}: 2, {10, 9, 4}: 2,
			},
		},
		{
			"three axes", AABB{1.620267506470761, 8.207228881623987, 3.252794543495013, 2.956064081231864, 8.527557252438134, 4.588591118256116},
			-0.7234810606282394, 0.9672327870217785, 1.0270837449875456,
			map[[3]int32]int{
				{3, 7, 2}: 0, {2, 7, 2}: 0, {3, 7, 3}: 0, {2, 7, 3}: 0, {3, 7, 4}: 1, {2, 7, 4}: 1,
				{3, 8, 3}: 2, {2, 8, 3}: 2, {1, 8, 3}: 2, {3, 8, 4}: 2, {2, 8, 4}: 2, {1, 8, 4}: 2,
			},
		},
		{
			"three axes, a step with nothing new", AABB{8.733779972454204, 11.236528813465359, 2.232533375587509, 8.964194194296592, 12.939318187645107, 2.462947597429897},
			0.7868277186858474, 1.4446370059350322, 1.871608702994481,
			map[[3]int32]int{
				{7, 9, 0}: 0, {8, 9, 0}: 0, {7, 10, 0}: 0, {8, 10, 0}: 0, {7, 11, 0}: 0, {8, 11, 0}: 0,
				{8, 10, 1}: 3, {8, 11, 1}: 3, {8, 10, 2}: 4, {8, 11, 2}: 4, {8, 12, 1}: 4, {8, 12, 2}: 4,
			},
		},
	} {
		got := map[[3]int32]int{}
		box := tc.box
		box.deflate(insideShave)
		forEachCellBetween(0, 0, 0, tc.dx, tc.dy, tc.dz, box, func(x, y, z int32, step int) bool {
			if _, dup := got[[3]int32{x, y, z}]; dup {
				t.Errorf("%s: cell %d %d %d visited twice", tc.name, x, y, z)
			}
			got[[3]int32{x, y, z}] = step
			return true
		})
		if !maps.Equal(got, tc.want) {
			t.Errorf("%s: cells = %v, want %v", tc.name, got, tc.want)
		}
	}
}
