package world

import (
	"testing"
)

// cboxF is CollisionBoxes' form of boxF, so the tables of expected shapes
// stay written in the vanilla sixteenths.
func cboxF(minX, minY, minZ, maxX, maxY, maxZ float64) CollisionBox {
	return CollisionBox{minX / 16, minY / 16, minZ / 16, maxX / 16, maxY / 16, maxZ / 16}
}

// TestCollisionBoxesOfTheModelledFamilies holds the families' own
// collision shapes, by state, each drawn from the vanilla class it
// comes from: a fence's connected sides arm its post, a wall stands its
// post on the up property and low and tall sides arm it alike, an open
// gate and an armless wall hold nothing at all, a chain is a thin column
// along its axis, and a chest's double half extends to the face it joins
// at. Every box is the vanilla one, sixteenths and all.
func TestCollisionBoxesOfTheModelledFamilies(t *testing.T) {
	t.Parallel()
	post := cboxF(6, 0, 6, 10, 24, 10)
	panePost := cboxF(7, 0, 7, 9, 16, 9)
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []CollisionBox
	}{
		{"minecraft:oak_fence", nil, []CollisionBox{post}},
		{"minecraft:nether_brick_fence", nil, []CollisionBox{post}},
		{"minecraft:oak_fence", map[string]string{"north": "true"},
			[]CollisionBox{post, cboxF(6, 0, 0, 10, 24, 6)}},
		{"minecraft:oak_fence", map[string]string{"east": "true"},
			[]CollisionBox{post, cboxF(10, 0, 6, 16, 24, 10)}},
		{"minecraft:oak_fence", map[string]string{
			"east": "true", "north": "true", "south": "true", "west": "true"},
			[]CollisionBox{post,
				cboxF(6, 0, 0, 10, 24, 6), cboxF(6, 0, 10, 10, 24, 16),
				cboxF(0, 0, 6, 6, 24, 10), cboxF(10, 0, 6, 16, 24, 10)}},
		{"minecraft:iron_bars", nil, []CollisionBox{panePost}},
		{"minecraft:copper_bars", map[string]string{"east": "true"},
			[]CollisionBox{panePost, cboxF(9, 0, 7, 16, 16, 9)}},
		{"minecraft:white_stained_glass_pane", map[string]string{"east": "true"},
			[]CollisionBox{panePost, cboxF(9, 0, 7, 16, 16, 9)}},
		{"minecraft:glass_pane", map[string]string{"north": "true", "west": "true"},
			[]CollisionBox{panePost, cboxF(7, 0, 0, 9, 16, 7), cboxF(0, 0, 7, 7, 16, 9)}},
		{"minecraft:cobblestone_wall", nil, []CollisionBox{cboxF(4, 0, 4, 12, 24, 12)}},
		{"minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "low", "south": "tall", "up": "false", "west": "none"},
			[]CollisionBox{cboxF(5, 0, 0, 11, 24, 11), cboxF(5, 0, 5, 11, 24, 16)}},
		{"minecraft:cobblestone_wall", map[string]string{
			"east": "tall", "north": "none", "south": "none", "up": "true", "west": "low"},
			[]CollisionBox{cboxF(4, 0, 4, 12, 24, 12), cboxF(0, 0, 5, 11, 24, 11), cboxF(5, 0, 5, 16, 24, 11)}},
		{"minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "none", "south": "none", "up": "false", "west": "none"},
			nil},
		{"minecraft:iron_chain", nil, []CollisionBox{cboxF(6.5, 0, 6.5, 9.5, 16, 9.5)}},
		{"minecraft:iron_chain", map[string]string{"axis": "x"},
			[]CollisionBox{cboxF(0, 6.5, 6.5, 16, 9.5, 9.5)}},
		{"minecraft:copper_chain", map[string]string{"axis": "z"},
			[]CollisionBox{cboxF(6.5, 6.5, 0, 9.5, 9.5, 16)}},
		{"minecraft:oak_fence_gate", nil, []CollisionBox{cboxF(0, 0, 6, 16, 24, 10)}},
		{"minecraft:oak_fence_gate", map[string]string{"facing": "west", "in_wall": "true"},
			[]CollisionBox{cboxF(6, 0, 0, 10, 24, 16)}},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, nil},
		{"minecraft:chest", nil, []CollisionBox{cboxF(1, 0, 1, 15, 14, 15)}},
		{"minecraft:chest", map[string]string{"facing": "north", "type": "left"},
			[]CollisionBox{cboxF(1, 0, 1, 16, 14, 15)}},
		{"minecraft:chest", map[string]string{"facing": "south", "type": "left"},
			[]CollisionBox{cboxF(0, 0, 1, 15, 14, 15)}},
		{"minecraft:chest", map[string]string{"facing": "east", "type": "right"},
			[]CollisionBox{cboxF(1, 0, 0, 15, 14, 15)}},
		{"minecraft:trapped_chest", nil, []CollisionBox{cboxF(1, 0, 1, 15, 14, 15)}},
		{"minecraft:copper_chest", nil, []CollisionBox{cboxF(1, 0, 1, 15, 14, 15)}},
		{"minecraft:ender_chest", map[string]string{"facing": "west"},
			[]CollisionBox{cboxF(1, 0, 1, 15, 14, 15)}},
	} {
		state := mustFind(t, tc.name, tc.props)
		got := CollisionBoxes(state)
		if len(got) != len(tc.want) {
			t.Errorf("boxes of %s %v = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("box %d of %s %v = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
		}
	}
}

// TestCollisionTopReadsTheModelledFamilies checks the height read the
// entity physics and the spawn placement stand on: the families with
// modelled boxes reach the tops of their boxes, everything else keeps the
// generated table's own per-state word.
func TestCollisionTopReadsTheModelledFamilies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  float64
	}{
		{"minecraft:stone", nil, 1},
		{"minecraft:oak_fence", nil, 1.5},
		{"minecraft:cobblestone_wall", nil, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, 0},
		{"minecraft:oak_fence_gate", nil, 1.5},
		{"minecraft:iron_chain", map[string]string{"axis": "x"}, 9.5 / 16},
		{"minecraft:chest", nil, 14.0 / 16},
		{"minecraft:farmland", nil, 15.0 / 16},
		{"minecraft:white_carpet", nil, 1.0 / 16},
		{"minecraft:flower_pot", nil, 6.0 / 16},
		{"minecraft:torch", nil, 0},
	} {
		if got := CollisionTop(mustFind(t, tc.name, tc.props)); got != tc.want {
			t.Errorf("CollisionTop(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
		}
	}
}
