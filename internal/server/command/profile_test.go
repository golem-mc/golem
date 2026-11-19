package command

import "testing"

// TestProfileComponentMalformedAcceptsTheCodecsShapes pins the profile value
// check to the shapes ResolvableProfile's codec reads: a bare player name, a
// map naming the name, the id or the property list, and the empty map. The id
// is an int array or a list of ints, and the properties are a list of
// property maps or one map of name to value list.
// vanilla: ResolvableProfile.CODEC, ExtraCodecs.STORED_GAME_PROFILE
func TestProfileComponentMalformedAcceptsTheCodecsShapes(t *testing.T) {
	accepted := []string{
		`"Notch"`,
		`""`,
		`{name:"Notch"}`,
		`{name:"aaaaaaaaaaaaaaaa"}`,
		`{id:[I;1,2,3,4]}`,
		`{id:[1,2,3,4]}`,
		`{}`,
		`{name:"Notch",id:[I;1,2,3,4]}`,
		`{properties:[{name:"n",value:"v"}]}`,
		`{properties:[]}`,
		`{properties:{}}`,
		`{properties:{textures:["abc"]}}`,
		`{name:"Notch",properties:[{name:"n",value:"v",signature:"s"}]}`,
		`{name:"Notch",texture:"entity/player/wide/alex"}`,
		`{name:"Notch",cape:"a",elytra:"b"}`,
		`{name:"Notch",model:"slim"}`,
		`{name:"Notch",model:"wide"}`,
	}
	for _, value := range accepted {
		if msg := componentValueMalformed("minecraft:profile", value); msg != "" {
			t.Errorf("componentValueMalformed(profile, %s) = %q, want accepted", value, msg)
		}
	}

	refused := []string{
		`"ThisNameIsWayTooLong"`,
		`"a b"`,
		`5`,
		`{name:"ThisNameIsWayTooLong"}`,
		`{name:"a b"}`,
		`{name:5}`,
		`{properties:5}`,
		`{properties:{textures:5}}`,
		`[1]`,
		`{name:"Notch",texture:"Bad Id"}`,
		`{name:"Notch",cape:"Bad Id"}`,
		`{name:"Notch",elytra:"Bad Id"}`,
		`{name:"Notch",texture:5}`,
		`{name:"Notch",model:"foo"}`,
		`{name:"Notch",model:"default"}`,
		`{name:"Notch",model:5}`,
	}
	for _, value := range refused {
		if msg := componentValueMalformed("minecraft:profile", value); msg == "" {
			t.Errorf("componentValueMalformed(profile, %s) accepted a value the codec refuses", value)
		}
	}
}

// TestProfileComponentMalformedWordsTheRefusals pins the exact wording of the
// two refusals the issue reported: a name past the 16 units the wire codec
// reads back, as a bare string and inside the map, which the client would
// otherwise drop the connection over.
// vanilla: ExtraCodecs.PLAYER_NAME
func TestProfileComponentMalformedWordsTheRefusals(t *testing.T) {
	const badLocation = "Not a valid resource location: Bad Id Non [a-z0-9/._-] character in path of location: minecraft:Bad Id"
	for _, tc := range []struct {
		value string
		want  string
	}{
		{
			`{name:"ThisNameIsWayTooLong"}`,
			`Failed to parse either. First: String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]; ` +
				`String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]; ` +
				`No key id in MapLike[{name:"ThisNameIsWayTooLong"}]; Second: Not a string`,
		},
		{
			`"ThisNameIsWayTooLong"`,
			`Failed to parse either. First: Not a map: "ThisNameIsWayTooLong"; ` +
				`Second: String "ThisNameIsWayTooLong" is too long: 20, expected range [0-16]`,
		},
		{
			`5`,
			`Failed to parse either. First: Not a map: 5; Second: Not a string`,
		},
		{
			`{properties:5}`,
			`Failed to parse either. First: Failed to parse either. First: Not a map: 5; Second: Not a list: 5; ` +
				`Failed to parse either. First: Not a map: 5; Second: Not a list: 5; ` +
				`No key name in MapLike[{properties:5}]; No key id in MapLike[{properties:5}]; Second: Not a string`,
		},
		{
			// A bad skin field alone is worded unwrapped: the game profile
			// part decodes, so the either around the two parts never came
			// into it.
			`{name:"Notch",texture:"Bad Id"}`,
			badLocation,
		},
		{
			`{name:"Notch",model:"foo"}`,
			`Unknown element name:foo`,
		},
		{
			`{name:"Notch",texture:5}`,
			`Not a string`,
		},
		{
			`{name:"Notch",model:5}`,
			`Not a string`,
		},
		{
			// Only the cape's refusal reads: the texture "a" is a location
			// the parser takes.
			`{name:"Notch",texture:"a",cape:"Bad Id"}`,
			badLocation,
		},
		{
			// The model's refusal comes before the texture's, the map codec
			// reading its fields back to front.
			`{name:"Notch",model:"foo",texture:"Bad Id"}`,
			`Unknown element name:foo; ` + badLocation,
		},
		{
			// Both parts refuse: the skin patch's refusal reads first, ahead
			// of the game profile's own repeated errors.
			`{name:"a b",texture:"Bad Id"}`,
			`Failed to parse either. First: ` + badLocation +
				`; Player name contained disallowed characters: 'a b'; ` +
				`Player name contained disallowed characters: 'a b'; ` +
				`No key id in MapLike[{name:"a b",texture:"Bad Id"}]; Second: Not a string`,
		},
		{
			`{name:"Notch",id:"x",texture:"Bad Id"}`,
			`Failed to parse either. First: ` + badLocation + `; Not a list; Not a list; Second: Not a string`,
		},
	} {
		if got := componentValueMalformed("minecraft:profile", tc.value); got != tc.want {
			t.Errorf("componentValueMalformed(profile, %s):\n got %q\nwant %q", tc.value, got, tc.want)
		}
	}
}
