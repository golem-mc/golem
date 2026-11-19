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
