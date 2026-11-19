package command

import (
	"fmt"
	"slices"
	"testing"
)

func TestParseVec3CentresWholeXAndZ(t *testing.T) {
	cases := []struct {
		words   []string
		x, y, z coord
	}{
		{[]string{"200", "64", "200"}, coord{value: 200.5}, coord{value: 64}, coord{value: 200.5}},
		{[]string{"200.0", "64.0", "200.0"}, coord{value: 200}, coord{value: 64}, coord{value: 200}},
		{[]string{"-3", "-60", "-7"}, coord{value: -2.5}, coord{value: -60}, coord{value: -6.5}},
		{[]string{"-3.25", "1", ".5"}, coord{value: -3.25}, coord{value: 1}, coord{value: 0.5}},
		{[]string{"5.", "1", "-0"}, coord{value: 5}, coord{value: 1}, coord{value: 0.5}},
		{[]string{"~", "~", "~"}, coord{relative: true}, coord{relative: true}, coord{relative: true}},
		{[]string{"~2", "~-1", "~-3"}, coord{relative: true, value: 2}, coord{relative: true, value: -1}, coord{relative: true, value: -3}},
	}
	for _, tc := range cases {
		p, ok := parseVec3(tc.words)
		want := position{x: tc.x, y: tc.y, z: tc.z}
		if !ok || p != want {
			t.Errorf("parseVec3(%q) = %+v %v, want %+v", tc.words, p, ok, want)
		}
	}
}

func TestParseBlockPos(t *testing.T) {
	cases := []struct {
		words []string
		want  position
	}{
		{[]string{"200", "64", "-7"}, position{x: coord{value: 200}, y: coord{value: 64}, z: coord{value: -7}}},
		// A relative offset may be a fraction though an absolute
		// coordinate may not.
		{[]string{"~1.5", "~", "~-0.5"}, position{x: coord{true, 1.5}, y: coord{relative: true}, z: coord{true, -0.5}}},
		{[]string{"^", "^1", "^-2.5"}, position{local: true, y: coord{value: 1}, z: coord{value: -2.5}}},
	}
	for _, tc := range cases {
		p, ok := parseBlockPos(tc.words)
		if !ok || p != tc.want {
			t.Errorf("parseBlockPos(%q) = %+v %v, want %+v", tc.words, p, ok, tc.want)
		}
	}
}

func TestParseVec3Local(t *testing.T) {
	p, ok := parseVec3([]string{"^1", "^", "^-.5"})
	want := position{local: true, x: coord{value: 1}, z: coord{value: -0.5}}
	if !ok || p != want {
		t.Errorf("parseVec3 = %+v %v, want %+v", p, ok, want)
	}
}

func TestParseRotationDoesNotCentre(t *testing.T) {
	yaw, pitch, ok := parseRotation([]string{"90", "10"})
	if !ok || yaw.value != 90 || pitch.value != 10 {
		t.Errorf("parseRotation = %v %v %v, want 90 10", yaw, pitch, ok)
	}
}

// Each case is a whole command with its coordinate argument at word arg, and
// the key, %s and cursor a vanilla server answered it with.
func TestParseCoordinateArgumentErrors(t *testing.T) {
	cases := []struct {
		input  string
		arg    int
		parser int32
		key    string
		with   []string
		cursor int
	}{
		{"summon minecraft:zombie 1.2.3 64 0", 2, ParserVec3, keyDoubleInvalid, []string{"1.2.3"}, 24},
		{"summon minecraft:zombie --1 64 0", 2, ParserVec3, keyDoubleInvalid, []string{"--1"}, 24},
		{"summon minecraft:zombie 1- 64 0", 2, ParserVec3, keyDoubleInvalid, []string{"1-"}, 24},
		{"summon minecraft:zombie +5 64 0", 2, ParserVec3, keyDoubleExpected, nil, 24},
		{"summon minecraft:zombie inf 64 0", 2, ParserVec3, keyDoubleExpected, nil, 24},
		{"summon minecraft:zombie 1e3 64 0", 2, ParserVec3, keyPos3DIncomplete, nil, 24},
		{"summon minecraft:zombie 0 64 1e3", 2, ParserVec3, keyExpectedSeparator, nil, 30},
		{"summon minecraft:zombie ~- 64 0", 2, ParserVec3, keyDoubleInvalid, []string{"-"}, 25},
		{"summon minecraft:zombie ~~ 64 0", 2, ParserVec3, keyDoubleExpected, nil, 25},
		{"summon minecraft:zombie 1 64", 2, ParserVec3, keyPos3DIncomplete, nil, 24},
		{"summon minecraft:zombie 1", 2, ParserVec3, keyPos3DIncomplete, nil, 24},
		{"summon minecraft:zombie ^ ^ 2", 2, ParserVec3, keyPosMixed, nil, 24},
		{"summon minecraft:zombie 1 ^ 2", 2, ParserVec3, keyPosMixed, nil, 26},
		{"summon minecraft:zombie ^1 ^x ^", 2, ParserVec3, keyDoubleExpected, nil, 28},
		{"summon minecraft:zombie ^ ^", 2, ParserVec3, keyPos3DIncomplete, nil, 24},
		{"setblock 1.5 64 0 stone", 1, ParserBlockPos, keyIntInvalid, []string{"1.5"}, 9},
		{"setblock 0 64 .5 stone", 1, ParserBlockPos, keyIntInvalid, []string{".5"}, 14},
		{"setblock 3000000000 64 0 stone", 1, ParserBlockPos, keyIntInvalid, []string{"3000000000"}, 9},
		{"setblock ~ ~ ^ stone", 1, ParserBlockPos, keyPosMixed, nil, 13},
		{"setblock - 64 0 stone", 1, ParserBlockPos, keyIntInvalid, []string{"-"}, 9},
		{"setblock x 64 0 stone", 1, ParserBlockPos, keyIntExpected, nil, 9},
		{"execute rotated 1.x 0 run say hi", 2, ParserRotation, keyRotIncomplete, nil, 16},
		{"execute rotated 1 run say hi", 2, ParserRotation, keyDoubleExpected, nil, 18},
		{"execute rotated ^ 0 run say hi", 2, ParserRotation, keyPosMixed, nil, 16},
		{"execute rotated 1", 2, ParserRotation, keyRotIncomplete, nil, 16},
	}
	for _, tc := range cases {
		tokens := Tokenize(tc.input)
		arity := 3
		if tc.parser == ParserRotation {
			arity = 2
		}
		err := parseCoordinateArgument(tc.parser, tokens[tc.arg:min(tc.arg+arity, len(tokens))])
		if err == nil {
			t.Errorf("%q: accepted", tc.input)
			continue
		}
		if err.Key != tc.key || !slices.Equal(err.With, tc.with) || err.Cursor != tc.cursor {
			t.Errorf("%q: got %s %q at %d, want %s %q at %d", tc.input, err.Key, err.With, err.Cursor, tc.key, tc.with, tc.cursor)
		}
	}
}

func TestParseCoordinateArgumentAccepts(t *testing.T) {
	cases := []struct {
		input  string
		parser int32
	}{
		{"1 2 3", ParserVec3},
		{"~ ~1 ~-.5", ParserVec3},
		{"^ ^1 ^-.5", ParserVec3},
		{"1. -.5 0", ParserVec3},
		{"~1.5 64 0", ParserBlockPos},
		{"^0.5 ^ ^2", ParserBlockPos},
		{"-5 64 -0", ParserBlockPos},
		{"90 ~-10", ParserRotation},
		{"~ ~", ParserRotation},
	}
	for _, tc := range cases {
		if err := parseCoordinateArgument(tc.parser, Tokenize(tc.input)); err != nil {
			t.Errorf("%q: %v", tc.input, err)
		}
	}
}

// Where a vanilla server put an execute positioned 0.5 <baseY> 0.5
// rotated <yaw> <pitch> run tp @s ^left ^up ^forwards.
func TestLocalOffsetMatchesVanilla(t *testing.T) {
	cases := []struct {
		yaw, pitch         float32
		left, up, forwards float64
		baseY              float64
		want               string
	}{
		{33, -21, 1.5, 2, 3, 100, "0.622947 102.942078 3.064968"},
		{-120.5, 45, -1, 0, 2.5, 101.62, "2.530443 99.852233 0.464485"},
		{30, 10, 0, 0, 1, 100, "0.007628 99.826394 1.352876"},
		{10, 20, 1, 2, 3, 100, "0.876620 100.853558 4.123534"},
		{90, -90, 0, 0, 0, 100, "0.500000 100.000000 0.500000"},
		{179.9, 89.9, 0.3, -0.7, 1.1, 100, "0.201272 98.898794 1.198646"},
		// A yaw past the long range narrows to the table's last entry,
		// not to whatever the conversion makes of it.
		{1e20, 0, 0, 0, 1, 100, "0.499904 100.000000 0.499904"},
	}
	for _, tc := range cases {
		dx, dy, dz := localOffset(tc.yaw, tc.pitch, tc.left, tc.up, tc.forwards)
		got := fmt.Sprintf("%s %s %s", formatCoord(0.5+dx), formatCoord(tc.baseY+dy), formatCoord(0.5+dz))
		if got != tc.want {
			t.Errorf("localOffset(%v, %v, %v %v %v) lands at %s, want %s", tc.yaw, tc.pitch, tc.left, tc.up, tc.forwards, got, tc.want)
		}
	}
}

func TestIsCoordish(t *testing.T) {
	for word, want := range map[string]bool{
		"1": true, "-.5": true, "~": true, "~2": true, "^": true, "^1": true,
		"@s": false, "Steve": false, "1.2.3": false, "1e3": false,
	} {
		if got := isCoordish(word); got != want {
			t.Errorf("isCoordish(%q) = %v, want %v", word, got, want)
		}
	}
}
