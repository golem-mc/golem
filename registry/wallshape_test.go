package registry

import "testing"

// TestAWallRaisesItsPostWhereVanillaDoes checks the post decision on its own:
// a lone wall, an end, a corner and a T are posts, a straight run of matching
// arms is not, and a post-forcing block above (a torch, a wall post) raises it
// again while a full cube above instead makes the arms tall and leaves the
// straight run flat.
func TestAWallRaisesItsPostWhereVanillaDoes(t *testing.T) {
	low, tall, none := wallLow, wallTall, wallNone
	cases := []struct {
		name                            string
		north, south, east, west        wallSide
		abovePostForcing, aboveFull, up bool
	}{
		{"lone wall", none, none, none, none, false, false, true},
		{"straight run north/south", low, low, none, none, false, false, false},
		{"straight run east/west", none, none, low, low, false, false, false},
		{"end", low, none, none, none, false, false, true},
		{"corner", low, none, low, none, false, false, true},
		{"T junction", low, low, low, none, false, false, true},
		{"four-way", low, low, low, low, false, false, false},
		{"straight run under a torch", low, low, none, none, true, false, true},
		{"straight run under a wall post", low, low, none, none, true, false, true},
		{"straight run under a full block", tall, tall, none, none, false, true, false},
	}
	for _, c := range cases {
		if got := shouldRaisePost(c.north, c.south, c.east, c.west, c.abovePostForcing, c.aboveFull); got != c.up {
			t.Errorf("shouldRaisePost(%s, %s, %s, %s, above post forcing %v, above full %v) = %v, want %v (%s)",
				c.north, c.south, c.east, c.west, c.abovePostForcing, c.aboveFull, got, c.up, c.name)
		}
	}
}
