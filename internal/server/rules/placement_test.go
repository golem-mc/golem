package rules

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestAxisFromFace checks the axis of a placed block from the face it was
// placed on: a log placed on the ground or against the ceiling stands up, and
// one placed against a wall lies along the wall, north and south deciding the
// z of the lying.
func TestAxisFromFace(t *testing.T) {
	cases := []struct {
		face int32
		axis string
	}{
		{protocol.FaceTop, "y"},
		{protocol.FaceBottom, "y"},
		{protocol.FaceNorth, "z"},
		{protocol.FaceSouth, "z"},
		{protocol.FaceWest, "x"},
		{protocol.FaceEast, "x"},
	}
	for _, c := range cases {
		if got := axisFromFace(c.face); got != c.axis {
			t.Errorf("axisFromFace(%d) = %q, want %q", c.face, got, c.axis)
		}
	}
}

// TestFacingFromFace checks the facing of a 6-directional block from the face
// it was placed against: a piston placed against the north wall points north,
// the way it would push, and one placed on the ground points up - into what
// stands above it.
func TestFacingFromFace(t *testing.T) {
	cases := []struct {
		face   int32
		facing string
	}{
		{protocol.FaceBottom, "down"},
		{protocol.FaceTop, "up"},
		{protocol.FaceNorth, "north"},
		{protocol.FaceSouth, "south"},
		{protocol.FaceWest, "west"},
		{protocol.FaceEast, "east"},
	}
	for _, c := range cases {
		if got := facingFromFace(c.face); got != c.facing {
			t.Errorf("facingFromFace(%d) = %q, want %q", c.face, got, c.facing)
		}
	}
}

// TestFacingFromPlayer checks the facing of a 4-directional block from the
// placing player's yaw: the block faces the player rather than the way they
// look, and a yaw that wraps past 360 or below 0 still lands in a quadrant -
// every quadrant is 90 degrees with its boundary at the 45-degree mark.
func TestFacingFromPlayer(t *testing.T) {
	cases := []struct {
		yaw    float32
		facing string
	}{
		{0, "north"},    // the player looks south, a furnace placed by them faces north
		{44.9, "north"}, // the quadrant holds to its 45-degree boundary
		{90, "east"},    // the player looks west
		{180, "south"},  // the player looks north
		{270, "west"},   // the player looks east
		{315, "north"},  // the wrap back to the first quadrant
		{-90, "west"},   // a negative yaw wraps, rather than missing a quadrant
		{-1, "north"},
		{720.5, "north"}, // anything past a whole turn folds back
	}
	for _, c := range cases {
		if got := facingFromPlayer(c.yaw); got != c.facing {
			t.Errorf("facingFromPlayer(%v) = %q, want %q", c.yaw, got, c.facing)
		}
	}
}

// TestPlayerDirection checks the direction the player looks in from their
// yaw: the vanilla Entity.getDirection, the opposite of facingFromPlayer,
// with the same wrap and 45-degree quadrant boundaries.
func TestPlayerDirection(t *testing.T) {
	cases := []struct {
		yaw       float32
		direction string
	}{
		{0, "south"},    // yaw 0 looks south
		{44.9, "south"}, // the quadrant holds to its 45-degree boundary
		{90, "west"},    // yaw 90 looks west
		{180, "north"},  // yaw 180 looks north
		{270, "east"},   // yaw 270 looks east
		{315, "south"},  // the wrap back to the first quadrant
		{-90, "east"},   // a negative yaw wraps, rather than missing a quadrant
		{-1, "south"},
		{720.5, "south"}, // anything past a whole turn folds back
	}
	for _, c := range cases {
		if got := PlayerDirection(c.yaw); got != c.direction {
			t.Errorf("PlayerDirection(%v) = %q, want %q", c.yaw, got, c.direction)
		}
	}
}

// TestHalfAndTypeFromFaceAndCursor checks the two height properties together,
// them being the same rule for neighbouring names: the face decides on its
// own when it is the floor or the ceiling, and a side face leaves it to the
// cursor - the top half only for a click strictly above the middle, the bottom half below.
func TestHalfAndTypeFromFaceAndCursor(t *testing.T) {
	cases := []struct {
		face    int32
		cursorY float32
		half    string
	}{
		{protocol.FaceTop, 0, "bottom"},
		{protocol.FaceTop, 1, "bottom"},
		{protocol.FaceBottom, 0, "top"},
		{protocol.FaceBottom, 1, "top"},
		{protocol.FaceNorth, 0.3, "bottom"},
		{protocol.FaceNorth, 0.5, "bottom"},
		{protocol.FaceNorth, 0.5001, "top"},
		{protocol.FaceSouth, 0.9, "top"},
	}
	for _, c := range cases {
		if got := halfFromFace(c.face, c.cursorY); got != c.half {
			t.Errorf("halfFromFace(%d, %v) = %q, want %q", c.face, c.cursorY, got, c.half)
		}
		if got := typeFromCursor(c.face, c.cursorY); got != c.half {
			t.Errorf("typeFromCursor(%d, %v) = %q, want %q", c.face, c.cursorY, got, c.half)
		}
	}
}

// TestRotationFromYaw checks the 22.5-degree steps of a placed sign or banner,
// which run clockwise from south at 0: west at 4, north at 8, east at 12, and
// a negative yaw or one past a turn folding back into the 16 steps.
func TestRotationFromYaw(t *testing.T) {
	cases := []struct {
		yaw      float32
		rotation string
	}{
		{0, "0"},
		{22.5, "1"},
		{90, "4"},
		{180, "8"},
		{270, "12"},
		{337.5, "15"},
		{-22.5, "15"},
		{361, "0"},
	}
	for _, c := range cases {
		if got := rotationFromYaw(c.yaw); got != c.rotation {
			t.Errorf("rotationFromYaw(%v) = %q, want %q", c.yaw, got, c.rotation)
		}
	}
}

// TestPlacementOverridesPickRealBlocksProperties checks the overrides against
// the real blocks each rule shows in: a log's axis, the two kinds of facing,
// a slab's type, a stair's half. Each set of overrides must also resolve to a
// state the registry knows, since that is what the server places by them.
func TestPlacementOverridesPickRealBlocksProperties(t *testing.T) {
	cases := []struct {
		name     string
		ctx      PlacementContext
		want     map[string]string
		excluded []string
	}{
		{
			name: "minecraft:oak_log",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 180, CursorY: 0.2},
			want: map[string]string{"axis": "y"},
		},
		{
			name: "minecraft:oak_log",
			ctx:  PlacementContext{Face: protocol.FaceEast, Yaw: 180, CursorY: 0.2},
			want: map[string]string{"axis": "x"},
		},
		{
			// A piston's facing is the player's look direction reversed —
			// the head toward the placer — not the face clicked.
			name: "minecraft:piston",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 90, CursorY: 0.2},
			want: map[string]string{"facing": "east"},
		},
		{
			// A furnace's facing runs four ways, so the player's yaw does.
			name: "minecraft:furnace",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 90, CursorY: 0.2},
			want: map[string]string{"facing": "east"},
		},
		{
			name: "minecraft:oak_slab",
			ctx:  PlacementContext{Face: protocol.FaceNorth, CursorY: 0.7},
			want: map[string]string{"type": "top"},
		},
		{
			name: "minecraft:oak_slab",
			ctx:  PlacementContext{Face: protocol.FaceTop, CursorY: 0.9},
			want: map[string]string{"type": "bottom"},
		},
		{
			// A chest carries a "type" too, but it is a chest's and not a
			// slab's: the placement rules leave it at "single".
			name:     "minecraft:chest",
			ctx:      PlacementContext{Face: protocol.FaceNorth, Yaw: 0, CursorY: 0.7},
			want:     map[string]string{"facing": "north"},
			excluded: []string{"type"},
		},
		{
			// A stair faces the way the player looks: the tall half stands
			// toward the look, so a stair placed looking south climbs south.
			name: "minecraft:oak_stairs",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorY: 0.2},
			want: map[string]string{"facing": "south", "half": "bottom"},
		},
		{
			name: "minecraft:oak_stairs",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 180, CursorY: 0.2},
			want: map[string]string{"facing": "north", "half": "bottom"},
		},
	}
	for _, c := range cases {
		overrides := PlacementOverrides(blockNamed(c.name), c.ctx)
		got := overrideMap(overrides)
		for prop, want := range c.want {
			if got[prop] != want {
				t.Errorf("PlacementOverrides(%s, %+v)[%s] = %q, want %q",
					c.name, c.ctx, prop, got[prop], want)
			}
		}
		for _, prop := range c.excluded {
			if _, has := got[prop]; has {
				t.Errorf("PlacementOverrides(%s, %+v) set %s, want it left at its default",
					c.name, c.ctx, prop)
			}
		}
		if _, ok := registry.WithProps(blockNamed(c.name).DefaultState(), overrides...); !ok {
			t.Errorf("PlacementOverrides(%s, %+v) = %v, which no state carries", c.name, c.ctx, overrides)
		}
	}
}

// TestCommandBlockPlacementUsesLook checks the three command blocks against
// CommandBlock.getStateForPlacement: FACING is the nearest looking direction
// reversed, so the pitch counts and the clicked face does not - a block
// placed while looking steeply down faces up whichever face was clicked, and
// a horizontal look faces back across the placer.
func TestCommandBlockPlacementUsesLook(t *testing.T) {
	cases := []struct {
		desc   string
		ctx    PlacementContext
		facing string
	}{
		{"looking steeply down", PlacementContext{Face: protocol.FaceTop, Yaw: 0, Pitch: 60}, "up"},
		{"a side click still reads the pitch", PlacementContext{Face: protocol.FaceNorth, Yaw: 0, Pitch: 60}, "up"},
		{"looking west", PlacementContext{Face: protocol.FaceTop, Yaw: 90, Pitch: 0}, "east"},
	}
	for _, name := range []string{
		"minecraft:command_block",
		"minecraft:chain_command_block",
		"minecraft:repeating_command_block",
	} {
		for _, c := range cases {
			overrides := PlacementOverrides(blockNamed(name), c.ctx)
			got := overrideMap(overrides)
			if got["facing"] != c.facing {
				t.Errorf("PlacementOverrides(%s, %s) facing = %q, want %q",
					name, c.desc, got["facing"], c.facing)
			}
			if _, ok := registry.WithProps(blockNamed(name).DefaultState(), overrides...); !ok {
				t.Errorf("PlacementOverrides(%s, %s) = %v, which no state carries", name, c.desc, overrides)
			}
		}
	}
}

// TestCrafterOrientation checks CrafterBlock.getStateForPlacement's
// ORIENTATION over every FrontAndTop: the front is the nearest looking
// direction reversed, the top is up for a horizontal front, the horizontal
// look when the front points up and its opposite when the front points down.
func TestCrafterOrientation(t *testing.T) {
	// Yaw reads through PlayerDirection: 0 south, 90 west, 180 north, 270
	// east.
	cases := []struct {
		desc string
		ctx  PlacementContext
		want string
	}{
		{"looking south, flat", PlacementContext{Yaw: 0, Pitch: 0}, "north_up"},
		{"looking west, flat", PlacementContext{Yaw: 90, Pitch: 0}, "east_up"},
		{"looking north, flat", PlacementContext{Yaw: 180, Pitch: 0}, "south_up"},
		{"looking east, flat", PlacementContext{Yaw: 270, Pitch: 0}, "west_up"},
		{"looking south, down", PlacementContext{Yaw: 0, Pitch: 60}, "up_south"},
		{"looking west, down", PlacementContext{Yaw: 90, Pitch: 60}, "up_west"},
		{"looking north, down", PlacementContext{Yaw: 180, Pitch: 60}, "up_north"},
		{"looking east, down", PlacementContext{Yaw: 270, Pitch: 60}, "up_east"},
		{"looking south, up", PlacementContext{Yaw: 0, Pitch: -60}, "down_north"},
		{"looking west, up", PlacementContext{Yaw: 90, Pitch: -60}, "down_east"},
		{"looking north, up", PlacementContext{Yaw: 180, Pitch: -60}, "down_south"},
		{"looking east, up", PlacementContext{Yaw: 270, Pitch: -60}, "down_west"},
		// A pitch just either side of the forty-five-degree flip: the
		// nearest look is still horizontal below it and vertical above it.
		{"south, just flat of the flip", PlacementContext{Yaw: 0, Pitch: 44}, "north_up"},
		{"south, just past the flip", PlacementContext{Yaw: 0, Pitch: 46}, "up_south"},
	}
	for _, c := range cases {
		if got := crafterOrientation(c.ctx); got != c.want {
			t.Errorf("crafterOrientation(%s) = %q, want %q", c.desc, got, c.want)
		}
	}
}

// TestJigsawOrientation checks JigsawBlock.getStateForPlacement on all six
// faces: the front is the clicked face; the top is up unless the front is
// vertical, then the opposite of the horizontal look.
func TestJigsawOrientation(t *testing.T) {
	cases := []struct {
		face int32
		yaw  float32
		want string
	}{
		{protocol.FaceNorth, 0, "north_up"},
		{protocol.FaceSouth, 0, "south_up"},
		{protocol.FaceEast, 0, "east_up"},
		{protocol.FaceWest, 90, "west_up"},
		{protocol.FaceTop, 0, "up_north"},
		{protocol.FaceTop, 90, "up_east"},
		{protocol.FaceBottom, 180, "down_south"},
		{protocol.FaceBottom, 270, "down_west"},
	}
	for _, c := range cases {
		overrides := PlacementOverrides(registry.BlockJigsaw, PlacementContext{Face: c.face, Yaw: c.yaw})
		got := overrideMap(overrides)
		if got["orientation"] != c.want {
			t.Errorf("jigsaw face %d yaw %v orientation = %q, want %q", c.face, c.yaw, got["orientation"], c.want)
		}
		if _, ok := registry.WithProps(registry.BlockJigsaw.DefaultState(), overrides...); !ok {
			t.Errorf("jigsaw overrides %v match no state", overrides)
		}
	}
}

// TestDoorStateWithHinge checks DoorBlock.getHinge: a lower-half door standing
// on either side of the placement mirrors the pair, the collision-full blocks
// beside the placement's two halves tip the balance toward the side the hinge
// swings against, and with neither deciding, the half of the face the click
// landed on does — the sign of the look's step saying which half is the right
// one. The door below is placed looking south, so the player's left is east.
func TestDoorStateWithHinge(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	door := defaultState(t, "minecraft:oak_door")
	slab, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "double"})
	if !ok {
		t.Fatal("the block registry has no double oak slab")
	}
	piston, ok := registry.FindState("minecraft:piston", map[string]string{"facing": "up"})
	if !ok {
		t.Fatal("the block registry has no retracted piston")
	}
	mud, ok := registry.FindState("minecraft:mud", nil)
	if !ok {
		t.Fatal("the block registry has no mud")
	}
	grate, ok := registry.FindState("minecraft:copper_grate", nil)
	if !ok {
		t.Fatal("the block registry has no copper grate")
	}

	cases := []struct {
		name  string
		walls [][3]int32
		// wallState is what the walls stand as; unset means stone.
		wallState uint16
		doors     [][3]int32
		ctx       PlacementContext
		want      string
	}{
		{
			name: "an open placement with a centred click takes the left",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want: "left",
		},
		{
			name:  "a wall beside the lower half's left side tips the hinge left",
			walls: [][3]int32{{1, 64, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:  "left",
		},
		{
			name:  "a wall beside the upper half's left side tips the hinge left too",
			walls: [][3]int32{{1, 65, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:  "left",
		},
		{
			name:  "a wall on the right tips the hinge right",
			walls: [][3]int32{{-1, 64, 0}, {-1, 65, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:  "right",
		},
		{
			name:      "a double slab on the right tips the hinge right",
			walls:     [][3]int32{{-1, 64, 0}, {-1, 65, 0}},
			wallState: slab,
			ctx:       PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:      "right",
		},
		{
			name:      "a retracted piston on the right tips the hinge right",
			walls:     [][3]int32{{-1, 64, 0}, {-1, 65, 0}},
			wallState: piston,
			ctx:       PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:      "right",
		},
		{
			// Mud's lowered collision is not a full block, however it looks.
			name:      "mud on the right leaves the hinge left",
			walls:     [][3]int32{{-1, 64, 0}, {-1, 65, 0}},
			wallState: mud,
			ctx:       PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:      "left",
		},
		{
			// A grate does not occlude, yet its collision fills the cell.
			name:      "a copper grate on the right tips the hinge right",
			walls:     [][3]int32{{-1, 64, 0}, {-1, 65, 0}},
			wallState: grate,
			ctx:       PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:      "right",
		},
		{
			name:  "walls on both sides leave the click to decide",
			walls: [][3]int32{{-1, 64, 0}, {-1, 65, 0}, {1, 64, 0}, {1, 65, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.8, CursorZ: 0.5},
			want:  "left",
		},
		{
			name:  "a door on the left makes the pair's mirror",
			doors: [][3]int32{{1, 64, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:  "right",
		},
		{
			// The door on the right decides before the click half does, so
			// the click near that side still reads the pair's mirror.
			name:  "a door on the right makes the pair's mirror",
			doors: [][3]int32{{-1, 64, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.2, CursorZ: 0.5},
			want:  "left",
		},
		{
			name:  "doors on both sides leave the click to decide",
			doors: [][3]int32{{-1, 64, 0}, {1, 64, 0}},
			ctx:   PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.5, CursorZ: 0.5},
			want:  "left",
		},
		{
			// The click half runs along the look's own axis, its step's sign
			// saying which half is the right one: a south look's right half
			// is the west one, a north look's the east one.
			name: "a click on the south look's west half is a right hinge",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 0, CursorX: 0.2, CursorZ: 0.5},
			want: "right",
		},
		{
			name: "a click on the north look's east half is a right hinge",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 180, CursorX: 0.8, CursorZ: 0.5},
			want: "right",
		},
		{
			name: "a click on the east look's south half is a right hinge",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 270, CursorX: 0.5, CursorZ: 0.8},
			want: "right",
		},
		{
			name: "a click on the west look's north half is a right hinge",
			ctx:  PlacementContext{Face: protocol.FaceTop, Yaw: 90, CursorX: 0.5, CursorZ: 0.2},
			want: "right",
		},
		{
			// The cursor names the clicked block; against a wall's north face
			// the click sits on the placement's own south edge, a block past
			// the cursor's reach — a west look reads that edge as its left.
			name: "a wall-face click reads the cursor off the face's block",
			ctx:  PlacementContext{Face: protocol.FaceNorth, Yaw: 90, CursorX: 0.5, CursorZ: 0},
			want: "left",
		},
		{
			name: "a click that replaced its target reads the cursor as it is",
			ctx:  PlacementContext{Face: protocol.FaceNorth, Yaw: 90, CursorX: 0.5, CursorZ: 0, ReplacingClickedOn: true},
			want: "right",
		},
	}
	for _, c := range cases {
		w := testWorld{}
		wall := stone
		if c.wallState != 0 {
			wall = c.wallState
		}
		for _, p := range c.walls {
			w.set(p[0], p[1], p[2], wall)
		}
		for _, p := range c.doors {
			w.set(p[0], p[1], p[2], door)
		}
		state, ok := DoorStateWithHinge(w, door, c.ctx, 0, 64, 0)
		if !ok {
			t.Errorf("%s: no state came back", c.name)
			continue
		}
		_, props, _ := registry.BlockStateName(state)
		if got := registry.ParseStateProps(props)["hinge"]; got != c.want {
			t.Errorf("%s: hinge = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDoorStateWithPower checks DoorBlock.getStateForPlacement's signal read:
// a door placed where it is powered takes open=true and powered=true, and one
// placed without a signal stays shut and unpowered.
func TestDoorStateWithPower(t *testing.T) {
	door := defaultState(t, "minecraft:oak_door")
	for _, powered := range []bool{false, true} {
		state := DoorStateWithPower(door, powered)
		_, raw, ok := registry.BlockStateName(state)
		if !ok {
			t.Fatalf("DoorStateWithPower(powered=%v) returned an unknown state", powered)
		}
		props := registry.ParseStateProps(raw)
		want := strconv.FormatBool(powered)
		if props["open"] != want || props["powered"] != want {
			t.Errorf("DoorStateWithPower(powered=%v) = open=%q powered=%q, want both %q",
				powered, props["open"], props["powered"], want)
		}
	}
}

func TestAnvilFacesAQuarterTurnFromTheLook(t *testing.T) {
	cases := []struct {
		yaw    float32
		facing string
	}{{0, "west"}, {90, "north"}, {180, "east"}, {270, "south"}}
	for _, name := range []string{"minecraft:anvil", "minecraft:chipped_anvil", "minecraft:damaged_anvil"} {
		for _, c := range cases {
			got := overrideMap(PlacementOverrides(blockNamed(name), PlacementContext{Face: protocol.FaceTop, Yaw: c.yaw}))
			if got["facing"] != c.facing {
				t.Errorf("%s at yaw %v faces %q, want %q", name, c.yaw, got["facing"], c.facing)
			}
		}
	}
}

// TestWaterloggedForPlacement checks the fluid read a waterloggable block's
// placement makes: a source waterlogs and a flow of level 1-7 does not, the
// coral family and the conduit are waterlogged by a falling column (level 8
// and up) too, the campfire arrives unlit when the water waterlogs it, the
// light and a block without the property are left alone, and a block placed
// onto its own kind keeps the stacking rule's state.
func TestWaterloggedForPlacement(t *testing.T) {
	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("the block registry has no %s with %v", name, props)
		}
		return s
	}
	water := func(level string) uint16 {
		return state("minecraft:water", map[string]string{"level": level})
	}
	cases := []struct {
		what string
		name string
		cell uint16
		want map[string]string
		ok   bool
	}{
		{"a fence in a source is waterlogged", "minecraft:oak_fence", water("0"), map[string]string{"waterlogged": "true"}, true},
		{"a fence in flowing water stays dry", "minecraft:oak_fence", water("1"), map[string]string{"waterlogged": "false"}, true},
		{"a fence in falling water stays dry", "minecraft:oak_fence", water("8"), map[string]string{"waterlogged": "false"}, true},
		{"a coral in a source is waterlogged", "minecraft:tube_coral", water("0"), map[string]string{"waterlogged": "true"}, true},
		{"a coral in flowing water stays dry", "minecraft:tube_coral", water("1"), map[string]string{"waterlogged": "false"}, true},
		{"a coral in falling water is waterlogged", "minecraft:tube_coral", water("8"), map[string]string{"waterlogged": "true"}, true},
		{"a coral in falling water at level 9 is waterlogged", "minecraft:tube_coral", water("9"), map[string]string{"waterlogged": "true"}, true},
		{"a coral in falling water at level 15 is waterlogged", "minecraft:tube_coral", water("15"), map[string]string{"waterlogged": "true"}, true},
		{"a coral on dry land stays dry", "minecraft:tube_coral", 0, map[string]string{"waterlogged": "false"}, true},
		{"a coral wall fan in falling water is waterlogged", "minecraft:tube_coral_wall_fan", water("8"), map[string]string{"waterlogged": "true"}, true},
		{"a dead coral wall fan in flowing water stays dry", "minecraft:dead_horn_coral_wall_fan", water("3"), map[string]string{"waterlogged": "false"}, true},
		{"a conduit in falling water is waterlogged", "minecraft:conduit", water("8"), map[string]string{"waterlogged": "true"}, true},
		{"a conduit in falling water at level 15 is waterlogged", "minecraft:conduit", water("15"), map[string]string{"waterlogged": "true"}, true},
		{"a campfire in a source is unlit", "minecraft:campfire", water("0"), map[string]string{"waterlogged": "true", "lit": "false"}, true},
		{"a campfire in flowing water stays lit", "minecraft:campfire", water("1"), map[string]string{"waterlogged": "false", "lit": "true"}, true},
		{"a light in a source stays dry", "minecraft:light", water("0"), nil, false},
		{"a block without the property is refused", "minecraft:stone", water("0"), nil, false},
		{"a slab doubling onto itself keeps the stacking state", "minecraft:oak_slab",
			state("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"}), nil, false},
		{"a candle stacking onto itself keeps the stacking state", "minecraft:candle",
			state("minecraft:candle", map[string]string{"candles": "1", "lit": "false", "waterlogged": "true"}), nil, false},
		{"a sea pickle stacking onto itself keeps the stacking state", "minecraft:sea_pickle",
			state("minecraft:sea_pickle", map[string]string{"pickles": "1", "waterlogged": "true"}), nil, false},
	}
	for _, c := range cases {
		w := testWorld{}
		w.set(0, 64, 0, c.cell)
		placed := defaultState(t, c.name)
		got, ok := WaterloggedForPlacement(w, blockNamed(c.name), placed, 0, 64, 0)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.what, ok, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		_, raw, _ := registry.BlockStateName(got)
		props := registry.ParseStateProps(raw)
		for prop, want := range c.want {
			if props[prop] != want {
				t.Errorf("%s: %s = %q, want %q", c.what, prop, props[prop], want)
			}
		}
	}
}

// TestBlockStateFromItemComponent checks the state properties the placed
// stack's minecraft:block_state component names as they are applied to the
// placement's own state: a property the block carries takes the value the
// stack names, a property or value the block does not take is passed by
// without touching the rest, a block with no properties answers nothing,
// and empty properties leave the state alone.
// vanilla: BlockItem.updateBlockStateFromTag, BlockItemStateProperties.apply
func TestBlockStateFromItemComponent(t *testing.T) {
	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("the block registry has no %s with %v", name, props)
		}
		return s
	}
	lights := func(level string) uint16 {
		return state("minecraft:light", map[string]string{"level": level})
	}
	placed := lights("15")
	oriented := state("minecraft:furnace", map[string]string{"facing": "north"})
	cases := []struct {
		what   string
		name   string
		placed uint16
		props  map[string]string
		want   uint16
	}{
		{"a light takes the level the stack names", "minecraft:light",
			placed, map[string]string{"level": "5"}, lights("5")},
		{"a light takes level zero", "minecraft:light",
			placed, map[string]string{"level": "0"}, lights("0")},
		{"a light takes the waterlogged the stack names", "minecraft:light",
			placed, map[string]string{"level": "5", "waterlogged": "true"},
			state("minecraft:light", map[string]string{"level": "5", "waterlogged": "true"})},
		{"a value the level does not take is passed by", "minecraft:light",
			placed, map[string]string{"level": "16"}, placed},
		{"a property the light does not carry is passed by", "minecraft:light",
			placed, map[string]string{"snowy": "true"}, placed},
		{"the valid property applies around the invalid one", "minecraft:light",
			placed, map[string]string{"level": "5", "snowy": "true"}, lights("5")},
		{"a block with no properties answers nothing", "minecraft:stone",
			state("minecraft:stone", nil),
			map[string]string{"level": "5", "facing": "north"}, state("minecraft:stone", nil)},
		{"an oriented state is what the properties land on", "minecraft:furnace",
			oriented, map[string]string{"facing": "east"},
			state("minecraft:furnace", map[string]string{"facing": "east"})},
		{"a facing the light does not carry is passed by", "minecraft:light",
			placed, map[string]string{"facing": "east"}, placed},
		{"empty properties leave the placement's state", "minecraft:light",
			placed, nil, placed},
	}
	for _, c := range cases {
		if got := BlockStateFromItemComponent(c.placed, c.props); got != c.want {
			t.Errorf("%s: state = %d, want %d", c.what, got, c.want)
		}
	}
}

func TestPlacementFacesLookDirection(t *testing.T) {
	for _, name := range []string{
		"minecraft:campfire", "minecraft:soul_campfire", "minecraft:decorated_pot",
		"minecraft:oak_fence_gate", "minecraft:calibrated_sculk_sensor",
	} {
		for yaw, want := range map[float32]string{0: "south", 90: "west", 180: "north", 270: "east"} {
			got := overrideMap(PlacementOverrides(blockNamed(name), PlacementContext{Yaw: yaw, Face: protocol.FaceTop}))["facing"]
			if got != want {
				t.Errorf("%s at yaw %v: facing %q, want %q", name, yaw, got, want)
			}
		}
	}
}

// overrideMap reads placement overrides by property name.
func overrideMap(pvs []registry.PropValue) map[string]string {
	m := map[string]string{}
	for _, pv := range pvs {
		m[pv.P.Name()] = pv.V
	}
	return m
}
