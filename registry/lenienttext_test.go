package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestLenientTextOfDropsUnreadableStyleFields(t *testing.T) {
	cases := []struct {
		name string
		tag  nbt.Tag
		want nbt.Tag
	}{
		{
			name: "hover without value",
			tag: nbt.Compound{
				"text":        nbt.String("Bob"),
				"hover_event": nbt.Compound{"action": nbt.String("show_text")},
			},
			want: nbt.String("Bob"),
		},
		{
			name: "hover with valid sibling style preserved",
			tag: nbt.Compound{
				"text":        nbt.String("Bob"),
				"bold":        nbt.Byte(1),
				"hover_event": nbt.Compound{"action": nbt.String("show_text")},
			},
			want: nbt.Compound{"text": nbt.String("Bob"), "bold": nbt.Byte(1)},
		},
		{
			name: "bad color, shadow_color, font, insertion, bool flags, click_event",
			tag: nbt.Compound{
				"text":          nbt.String("Clean"),
				"color":         nbt.String("not_a_color"),
				"shadow_color":  nbt.String("bad"),
				"font":          nbt.String("BAD FONT"),
				"insertion":     nbt.Int(42),
				"bold":          nbt.String("yes"),
				"italic":        nbt.String("no"),
				"underlined":    nbt.Compound{},
				"strikethrough": nbt.List{},
				"obfuscated":    nbt.String("nope"),
				"click_event":   nbt.Compound{"action": nbt.String("open_file"), "path": nbt.String("/tmp")},
			},
			want: nbt.String("Clean"),
		},
		{
			name: "valid color kept when click_event dropped",
			tag: nbt.Compound{
				"text":        nbt.String("Red"),
				"color":       nbt.String("red"),
				"click_event": nbt.Compound{"action": nbt.String("change_page"), "page": nbt.Int(0)},
			},
			want: nbt.Compound{"text": nbt.String("Red"), "color": nbt.String("red")},
		},
		{
			name: "recurses into extra",
			tag: nbt.Compound{
				"text": nbt.String("A"),
				"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{
						"text":        nbt.String("B"),
						"hover_event": nbt.Compound{"action": nbt.String("show_text")},
					},
				}},
			},
			want: nbt.Compound{
				"text":  nbt.String("A"),
				"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.String("B")}},
			},
		},
		{
			name: "recurses into translate with",
			tag: nbt.Compound{
				"translate": nbt.String("chat.type.text"),
				"with": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"text": nbt.String("Alice"), "color": nbt.String("bogus")},
					nbt.Compound{"": nbt.Int(3)},
				}},
			},
			want: nbt.Compound{
				"translate": nbt.String("chat.type.text"),
				"with":      nbt.NewList([]nbt.Tag{nbt.String("Alice"), nbt.Int(3)}),
			},
		},
		{
			name: "recurses into show_text hover value",
			tag: nbt.Compound{
				"text": nbt.String("Tip"),
				"hover_event": nbt.Compound{
					"action": nbt.String("show_text"),
					"value": nbt.Compound{
						"text":  nbt.String("Inner"),
						"color": nbt.String("bogus"),
					},
				},
			},
			want: nbt.Compound{
				"text": nbt.String("Tip"),
				"hover_event": nbt.Compound{
					"action": nbt.String("show_text"),
					"value":  nbt.String("Inner"),
				},
			},
		},
		{
			name: "selector content preserved with bad hover dropped",
			tag: nbt.Compound{
				"selector":    nbt.String("@p"),
				"hover_event": nbt.Compound{"action": nbt.String("show_text")},
			},
			want: nbt.Compound{"selector": nbt.String("@p")},
		},
		{
			name: "valid custom click_event kept, invalid custom click_event dropped",
			tag: nbt.Compound{
				"text":        nbt.String("Act"),
				"click_event": nbt.Compound{"action": nbt.String("custom"), "id": nbt.String("demo:run")},
			},
			want: nbt.Compound{
				"text":        nbt.String("Act"),
				"click_event": nbt.Compound{"action": nbt.String("custom"), "id": nbt.String("demo:run")},
			},
		},
		{
			name: "invalid custom click_event dropped",
			tag: nbt.Compound{
				"text":        nbt.String("Act"),
				"click_event": nbt.Compound{"action": nbt.String("custom"), "id": nbt.String("BAD ID")},
			},
			want: nbt.String("Act"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := protocol.LenientTextOf(tc.tag)
			if !ok {
				t.Fatalf("LenientTextOf(%v) = _, false, want true", tc.tag)
			}
			gotSNBT := nbt.Stringify(protocol.TextTag(got))
			wantSNBT := nbt.Stringify(tc.want)
			if gotSNBT != wantSNBT {
				t.Errorf("protocol.TextTag(LenientTextOf) = %s, want %s", gotSNBT, wantSNBT)
			}
		})
	}
}

func TestLenientTextOfRefusesUnreadableContent(t *testing.T) {
	bad := []nbt.Tag{
		nil,
		nbt.Int(5),
		nbt.Compound{},
		nbt.Compound{"foo": nbt.String("bar")},
		nbt.Compound{"text": nbt.Int(1)},
		nbt.List{Elem: nbt.TypeString},
		nbt.Compound{
			"text":  nbt.String("A"),
			"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"text": nbt.Int(1)}}},
		},
		nbt.Compound{
			"translate": nbt.String("k"),
			"with":      nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"foo": nbt.Int(1)}}},
		},
	}
	for _, tag := range bad {
		if got, ok := protocol.LenientTextOf(tag); ok {
			t.Errorf("LenientTextOf(%v) = %+v, true, want false", tag, got)
		}
	}
}
