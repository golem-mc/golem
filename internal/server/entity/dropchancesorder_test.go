package entity

import "testing"

// Native HashMap populations recorded by tools/java/DropChancesOrder.java (26.3).
// Both keep all eight slots until filterDefaultValues removes the defaults.
func TestDropChancesMapPopulation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		hashes [8]uint32
		want   [8]int
	}{
		{[8]uint32{1768253416, 1105563869, 1719133872, 45422147, 2017231980, 355635406, 1286190782, 1711580774}, [8]int{4, 7, 3, 2, 6, 1, 5, 0}},
		{[8]uint32{1897221921, 368153528, 622263400, 810196347, 1811787796, 1921553024, 1617937669, 78265654}, [8]int{3, 0, 5, 1, 4, 6, 7, 2}},
	} {
		order := NewDropChancesOrder(tc.hashes)
		if order.slots != tc.want {
			t.Errorf("native population = %v, want %v", order.slots, tc.want)
		}
	}
}
