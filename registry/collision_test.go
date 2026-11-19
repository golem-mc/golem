package registry

import "testing"

func TestCollisionHeightReadsShapesOwnTops(t *testing.T) {
	for _, tc := range []struct {
		state string
		props map[string]string
		want  float64
	}{
		// The flat whole-floor shapes, tops the jar reports.
		{"minecraft:repeater", nil, 2.0 / 16},
		{"minecraft:comparator", nil, 2.0 / 16},
		{"minecraft:daylight_detector", nil, 6.0 / 16},
		{"minecraft:campfire", nil, 7.0 / 16},
		{"minecraft:sculk_sensor", nil, 8.0 / 16},
		{"minecraft:sculk_shrieker", nil, 8.0 / 16},
		{"minecraft:enchanting_table", nil, 12.0 / 16},
		{"minecraft:soul_sand", nil, 14.0 / 16},
		{"minecraft:farmland", nil, 15.0 / 16},
		{"minecraft:dirt_path", nil, 15.0 / 16},
		{"minecraft:white_carpet", nil, 1.0 / 16},
		{"minecraft:moss_carpet", nil, 1.0 / 16},
		{"minecraft:pale_moss_carpet", map[string]string{
			"bottom": "true", "east": "none", "north": "none",
			"south": "none", "west": "none"}, 1.0 / 16},
		{"minecraft:oak_trapdoor", map[string]string{
			"facing": "north", "half": "bottom", "open": "false",
			"powered": "false", "waterlogged": "false"}, 3.0 / 16},
		{"minecraft:end_portal_frame", map[string]string{
			"eye": "false", "facing": "north"}, 13.0 / 16},
		{"minecraft:oak_slab", map[string]string{"type": "bottom"}, 0.5},
		{"minecraft:stone", nil, 1},
		// The shapes that are neither one whole-floor box nor taller
		// than their cell read as the whole cell; the tall ones read
		// their full reach.
		{"minecraft:chest", nil, 1},
		{"minecraft:lily_pad", nil, 1},
		{"minecraft:oak_fence", nil, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "false"}, 1.5},
		{"minecraft:cobblestone_wall", map[string]string{
			"east": "low", "north": "none", "south": "none",
			"up": "true", "waterlogged": "false", "west": "none"}, 1.5},
		// The empty collision shapes.
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, 0},
		{"minecraft:torch", nil, 0},
		{"minecraft:light", nil, 0},
		{"minecraft:bush", nil, 0},
		{"minecraft:moving_piston", nil, 0},
	} {
		state := MustState(tc.state, tc.props)
		if got := CollisionHeight(state); got != tc.want {
			t.Errorf("CollisionHeight(%s %v) = %v, want %v", tc.state, tc.props, got, tc.want)
		}
	}
}

func TestCollisionHeightReadsSnowByTheLayer(t *testing.T) {
	for _, tc := range []struct {
		layers string
		want   float64
	}{
		{"1", 0},
		{"2", 2.0 / 16},
		{"4", 6.0 / 16},
		{"8", 14.0 / 16},
	} {
		state := MustState("minecraft:snow", map[string]string{"layers": tc.layers})
		if got := CollisionHeight(state); got != tc.want {
			t.Errorf("CollisionHeight(snow[layers=%s]) = %v, want %v", tc.layers, got, tc.want)
		}
	}
}

func TestCollisionHeightReadsNothingInTheUnknownState(t *testing.T) {
	if got := CollisionHeight(65535); got != 0 {
		t.Errorf("CollisionHeight(65535) = %v, want 0", got)
	}
}

// TestNoCollisionHoldsOnlyBlocksWithEmptyShapeInEveryState pins the set to
// the shapes: a block with a collision shape in any state stays out of it.
func TestNoCollisionHoldsOnlyBlocksWithEmptyShapeInEveryState(t *testing.T) {
	for name, want := range map[string]bool{
		"minecraft:air":              true,
		"minecraft:torch":            true,
		"minecraft:poppy":            true,
		"minecraft:bush":             true,
		"minecraft:frogspawn":        true,
		"minecraft:white_carpet":     false,
		"minecraft:black_carpet":     false,
		"minecraft:lily_pad":         false,
		"minecraft:tube_coral_block": false,
		"minecraft:scaffolding":      false,
		"minecraft:sea_pickle":       false,
		"minecraft:pitcher_crop":     false,
		"minecraft:oak_fence_gate":   false, // empty only while open
		"minecraft:snow":             false, // empty only at one layer
		"minecraft:stone":            false,
	} {
		if got := NoCollision[name]; got != want {
			t.Errorf("NoCollision[%s] = %v, want %v", name, got, want)
		}
	}
}
