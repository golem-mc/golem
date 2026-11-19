package world

import "testing"

func TestDripstoneWindOffset(t *testing.T) {
	w := dripstoneWind{originY: 10, maxOffset: 4, x: .3, z: -.2}
	for _, tc := range []struct{ pos, want treePos }{
		{treePos{8, 10, 8}, treePos{8, 10, 8}},
		{treePos{8, 9, 8}, treePos{8, 9, 7}},
		{treePos{8, 11, 8}, treePos{7, 11, 8}},
		{treePos{8, -30, 8}, treePos{12, -30, 4}},
		{treePos{8, 50, 8}, treePos{4, 50, 12}},
	} {
		if got := w.offset(tc.pos); got != tc.want {
			t.Errorf("offset(%v) = %v, want %v", tc.pos, got, tc.want)
		}
		if got := (dripstoneWind{}).offset(tc.pos); got != tc.pos {
			t.Errorf("no wind moved %v to %v", tc.pos, got)
		}
	}
}
