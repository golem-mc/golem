package rules

import "testing"

// The sign side table: where a player stands around each kind of sign, and
// which side that leaves them looking at. The coordinates are the ones
// vanilla's own geometry hands isFacingFrontText: the panel centre against
// the bearin from it to the player.
func TestSignFrontFacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		block     string
		props     map[string]string
		x, z      int32
		px, pz    float64
		wantFront bool
	}{
		{
			// A rotation-0 standing sign faces south: the player south of
			// it sees the front, the one north of it the back.
			name: "standing front", block: "minecraft:oak_sign", props: map[string]string{"rotation": "0", "waterlogged": "false"},
			x: 4, z: 7, px: 4.5, pz: 8.5, wantFront: true,
		},
		{
			name: "standing back", block: "minecraft:oak_sign", props: map[string]string{"rotation": "0", "waterlogged": "false"},
			x: 4, z: 7, px: 4.5, pz: 5.5, wantFront: false,
		},
		{
			// Rotation 8 faces north.
			name: "standing turned", block: "minecraft:oak_sign", props: map[string]string{"rotation": "8", "waterlogged": "false"},
			x: 0, z: 0, px: 0.5, pz: -2, wantFront: true,
		},
		{
			// Rotation 4 faces west.
			name: "standing west", block: "minecraft:oak_sign", props: map[string]string{"rotation": "4"},
			x: 0, z: 0, px: -3, pz: 0.5, wantFront: true,
		},
		{
			// Rotation 4's back is east.
			name: "standing east", block: "minecraft:oak_sign", props: map[string]string{"rotation": "4"},
			x: 0, z: 0, px: 2, pz: 0.5, wantFront: false,
		},
		{
			// A wall sign facing north hangs on the wall south of it: its
			// panel sits against that wall, and the north stands before the
			// front.
			name: "wall front", block: "minecraft:oak_wall_sign", props: map[string]string{"facing": "north"},
			x: 4, z: 7, px: 4.5, pz: 5.5, wantFront: true,
		},
		{
			name: "wall back", block: "minecraft:oak_wall_sign", props: map[string]string{"facing": "north"},
			x: 4, z: 7, px: 4.5, pz: 8.5, wantFront: false,
		},
		{
			// Facing east, the player east of the sign sees the front.
			name: "wall east", block: "minecraft:oak_wall_sign", props: map[string]string{"facing": "east"},
			x: 0, z: 0, px: 2.5, pz: 0.5, wantFront: true,
		},
		{
			// Facing south: the panel centre moves off the north wall to
			// the south one, which the closer player south sees around.
			name: "wall south", block: "minecraft:oak_wall_sign", props: map[string]string{"facing": "south"},
			x: 0, z: 0, px: 0.5, pz: 2.5, wantFront: true,
		},
		{
			// A ceiling-hanging sign turns like a standing one.
			name: "hanging front", block: "minecraft:oak_hanging_sign", props: map[string]string{"rotation": "0", "attached": "false"},
			x: 4, z: 7, px: 4.5, pz: 8.5, wantFront: true,
		},
		{
			// A wall-hanging sign faces like a wall sign.
			name: "wall hanging back", block: "minecraft:oak_wall_hanging_sign", props: map[string]string{"facing": "west"},
			x: 0, z: 0, px: 2, pz: 0.5, wantFront: false,
		},
		{
			// A wall-hanging sign keeps the cell centre, not the wall
			// sign's offset panel: a player just south of the centre reads
			// as the back, where the offset centre would call them front.
			name: "wall hanging centre", block: "minecraft:oak_wall_hanging_sign", props: map[string]string{"facing": "north"},
			x: 4, z: 7, px: 4.5, pz: 7.7, wantFront: false,
		},
	}
	for _, tc := range tests {
		if got := SignFrontFacing(stateWith(t, tc.block, tc.props), tc.x, tc.z, tc.px, tc.pz); got != tc.wantFront {
			t.Errorf("%s: SignFrontFacing = %v, want %v", tc.name, got, tc.wantFront)
		}
	}
}

// The side threshold is a half turn: a bearing exactly 90° away still reads
// as the front, one past it does not.
func TestSignFrontFacingThreshold(t *testing.T) {
	t.Parallel()
	sign := stateWith(t, "minecraft:oak_sign", map[string]string{"rotation": "0"}) // the front faces south
	// Due east of the centre is a quarter turn off the front.
	centre := []float64{4.5, 6.5} // the sign at (4, 64, 6), its centre
	far := 10.0
	if !SignFrontFacing(sign, 4, 6, centre[0]+far, centre[1]) {
		t.Error("a bearing due east of a south-facing sign reads as the back, want the 90° front")
	}
	// A step north of due east tips past 90°.
	if SignFrontFacing(sign, 4, 6, centre[0]+far, centre[1]-0.1) {
		t.Error("a bearing past 90° reads as the front, want the back")
	}
}

// The §-code strip: one code pair per §, the code characters vanilla's
// pattern ranges over, and everything else standing as it was — in
// particular an unpaired § and a § before a letter outside the codes.
func TestStripFormatting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"§6Gold", "Gold"},
		{"§l§oBold italics§r", "Bold italics"},
		{"§rreset", "reset"},
		{"§", "§"},
		{"§xNot a code", "§xNot a code"},
		{"A§1B§2C", "ABC"},
		{"colour §achar and §zchar", "colour char and §zchar"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := StripFormatting(tc.in); got != tc.want {
			t.Errorf("StripFormatting(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
