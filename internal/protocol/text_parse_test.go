package protocol

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestParseTextJSON(t *testing.T) {
	for _, tc := range []struct {
		json    string
		want    nbt.Tag
		dropped []string
	}{
		{`"plain"`, nbt.String("plain"), nil},
		{" \"padded\" \n", nbt.String("padded"), nil},
		{
			`{"text":"a","color":"red","italic":true,"extra":["b",{"translate":"k","with":["x"]}]}`,
			nbt.Compound{"text": nbt.String("a"), "color": nbt.String("red"), "italic": nbt.Byte(1),
				"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"": nbt.String("b")},
					nbt.Compound{"translate": nbt.String("k"), "with": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("x")}}},
				}}},
			nil,
		},
		{`["a","b"]`, nbt.Compound{"text": nbt.String("a"), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("b")}}}, nil},
		{`{"text":"x","color":"#A0b1C2"}`, nbt.Compound{"text": nbt.String("x"), "color": nbt.String("#A0b1C2")}, nil},
		{
			`{"text":"x","bold":true,"strikethrough":1,"obfuscated":false,"underlined":0,"font":"minecraft:alt","insertion":"i"}`,
			nbt.Compound{"text": nbt.String("x"), "bold": nbt.Byte(1), "strikethrough": nbt.Byte(1), "obfuscated": nbt.Byte(0),
				"underlined": nbt.Byte(0), "font": nbt.String("minecraft:alt"), "insertion": nbt.String("i")},
			nil,
		},
		{`{"text":"x","shadow_color":4294967295}`, nbt.Compound{"text": nbt.String("x"), "shadow_color": nbt.Int(-1)}, nil},
		{
			`{"text":"x","shadow_color":[1,0.5,0,1]}`,
			nbt.Compound{"text": nbt.String("x"), "shadow_color": nbt.List{Elem: nbt.TypeFloat,
				Items: []nbt.Tag{nbt.Float(1), nbt.Float(0.5), nbt.Float(0), nbt.Float(1)}}},
			nil,
		},
		{`{"keybind":"key.jump"}`, nbt.Compound{"keybind": nbt.String("key.jump")}, nil},
		{`{"type":"keybind","keybind":"key.jump"}`, nbt.Compound{"keybind": nbt.String("key.jump")}, nil},
		{
			`{"translate":"k","fallback":"f","with":[3,true,5000000000,1.5]}`,
			nbt.Compound{"translate": nbt.String("k"), "fallback": nbt.String("f"), "with": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"": nbt.Int(3)}, nbt.Compound{"": nbt.Byte(1)}, nbt.Compound{"": nbt.Long(5000000000)}, nbt.Compound{"": nbt.Double(1.5)},
			}}},
			nil,
		},
		{
			`{"text":"x","click_event":{"action":"open_url","url":"https://x"},"extra":[{"text":"y","hover_event":{},"click_event":{}}]}`,
			nbt.Compound{"text": nbt.String("x"), "extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"text": nbt.String("y")}}}},
			[]string{"click_event", "hover_event"},
		},
	} {
		got, dropped, err := ParseTextJSON(tc.json)
		if err != nil {
			t.Errorf("%s: %v", tc.json, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %#v\nwant %#v", tc.json, got, tc.want)
		}
		if !reflect.DeepEqual(dropped, tc.dropped) {
			t.Errorf("%s: dropped %q, want %q", tc.json, dropped, tc.dropped)
		}
	}
}

// TestParseTextJSONRefuses checks the components the client's codec would
// fail on, and the content types the parser does not read.
func TestParseTextJSONRefuses(t *testing.T) {
	for _, bad := range []string{
		``, `plain`, `{}`, `[]`, `{"text":1}`, `"a" "b"`, `3`, `{"text":"x","extra":[]}`,
		`{"text":"x","color":"notacolor"}`, `{"text":"x","color":"#12345"}`, `{"text":"x","color":"#+12345"}`,
		`{"text":"x","color":1}`, `{"text":"x","bold":"yes"}`, `{"text":"x","font":"Bad Font"}`,
		`{"text":"x","shadow_color":"red"}`, `{"text":"x","shadow_color":[1,2,3]}`, `{"text":"x","insertion":1}`,
		`{"translate":"k","with":"x"}`, `{"translate":"k","with":[{"text":"x","color":"nope"}]}`,
		`{"score":{"name":"a","objective":"b"}}`, `{"selector":1}`, `{"nbt":"a","block":"0 0 0"}`,
		`{"type":"object","object":"atlas"}`, `{"object":"bogus","sprite":"a"}`, `{"object":"player","sprite":"a"}`,
		`{"type":"text","keybind":"k"}`, `{"type":"bogus","text":"x"}`,
		`["a",{"text":"b","color":"nope"}]`, `{"text":"x"}]`, `{"text":"x"}}`, `"x"]`, `{"text":"x"} 1`,
	} {
		if tag, _, err := ParseTextJSON(bad); err == nil {
			t.Errorf("%q parsed to %#v", bad, tag)
		}
	}
}

// TestParseTextJSONSelector checks that a selector component keeps its
// selector and separator.
// vanilla: SelectorContents.MAP_CODEC
func TestParseTextJSONSelector(t *testing.T) {
	tag, _, err := ParseTextJSON(`{"selector":"@p","separator":"; "}`)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := tag.(nbt.Compound)
	if !ok || c["selector"] != nbt.String("@p") || c["separator"] != nbt.String("; ") {
		t.Errorf("got %#v", tag)
	}
}

// TestParseTextJSONObject checks that an object component keeps its sprite,
// profile, hat and fallback, in the typed and the compact form.
func TestParseTextJSONObject(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want nbt.Compound
	}{
		{`{"object":"atlas","sprite":"item/diamond","atlas":"minecraft:items"}`,
			nbt.Compound{"object": nbt.String("atlas"), "sprite": nbt.String("item/diamond"), "atlas": nbt.String("minecraft:items")}},
		{`{"type":"object","sprite":"item/diamond","fallback":"D"}`,
			nbt.Compound{"sprite": nbt.String("item/diamond"), "fallback": nbt.String("D")}},
		{`{"player":{"name":"Notch","id":[1,2,3,4]},"hat":false}`,
			nbt.Compound{"player": nbt.Compound{"name": nbt.String("Notch"), "id": nbt.IntArray{1, 2, 3, 4}}, "hat": nbt.Byte(0)}},
		{`{"player":"Notch","hat":1}`,
			nbt.Compound{"player": nbt.String("Notch"), "hat": nbt.Byte(1)}},
	} {
		tag, _, err := ParseTextJSON(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if nbt.Stringify(tag) != nbt.Stringify(tc.want) {
			t.Errorf("%s parsed to %s, want %s", tc.in, nbt.Stringify(tag), nbt.Stringify(tc.want))
		}
	}
}
