package protocol

import (
	"maps"
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: Style.Serializer.MAP_CODEC, NbtOps.getBooleanValue
func TestTextStyleFlagsAcceptNumericTags(t *testing.T) {
	t.Parallel()
	for _, value := range []struct {
		name string
		tag  nbt.Tag
		on   bool
	}{
		{"byte", nbt.Byte(1), true},
		{"short", nbt.Short(-2), true},
		{"int", nbt.Int(1), true},
		{"long", nbt.Long(256), true},
		{"float", nbt.Float(0.25), true},
		{"double", nbt.Double(-0.25), true},
		{"byte_zero", nbt.Byte(0), false},
		{"short_zero", nbt.Short(0), false},
		{"int_zero", nbt.Int(0), false},
		{"long_zero", nbt.Long(0), false},
		{"float_zero", nbt.Float(0), false},
		{"double_zero", nbt.Double(0), false},
		{"negative_zero", nbt.Double(math.Copysign(0, -1)), false},
	} {
		for _, flag := range []string{"bold", "italic", "underlined", "strikethrough", "obfuscated"} {
			t.Run(flag+"/"+value.name, func(t *testing.T) {
				tag := nbt.Compound{"text": nbt.String("X"), flag: value.tag}
				modelled := flag == "italic" || flag == "underlined"
				if modelled {
					got, ok := TextOf(tag)
					if !ok || got.Italic != (flag == "italic" && value.on) || got.Underline != (flag == "underlined" && value.on) {
						t.Fatalf("TextOf(%v) = %+v, %v", tag, got, ok)
					}
				}
				got, ok := StyledTextOf(tag)
				if !ok {
					t.Fatalf("StyledTextOf(%v) refused a numeric flag", tag)
				}
				if !modelled || !value.on {
					if got.Raw == nil || !nbt.Equal(got.Raw, tag) {
						t.Fatalf("StyledTextOf(%v) = %+v, want the tag kept raw", tag, got)
					}
				} else if got.Raw != nil || got.Italic != (flag == "italic") || got.Underline != (flag == "underlined") {
					t.Fatalf("StyledTextOf(%v) = %+v, want the modelled flag", tag, got)
				}
				var b Buffer
				b.PutText(Text{Raw: tag})
				read, ok := ReadText(NewReader(b.Bytes()))
				if !ok || !TextEqual(read, got) {
					t.Errorf("ReadText(%v) = %+v, %v, want %+v", tag, read, ok, got)
				}
			})
		}
	}
}

func TestTextStyleFlagsRefuseNonNumericTags(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"bold", "italic", "underlined", "strikethrough", "obfuscated"} {
		for _, value := range []nbt.Tag{nbt.String("true"), nbt.Compound{}, nbt.List{Elem: nbt.TypeByte}, nbt.ByteArray{1}} {
			tag := nbt.Compound{"text": nbt.String("X"), flag: value}
			if got, ok := StyledTextOf(tag); ok {
				t.Errorf("StyledTextOf(%v) = %+v, want a non-numeric flag refused", tag, got)
			}
			if flag == "italic" || flag == "underlined" {
				if got, ok := TextOf(tag); ok {
					t.Errorf("TextOf(%v) = %+v, want a non-numeric flag refused", tag, got)
				}
			}
		}
	}
}

func TestRawTextFitsBoundsNesting(t *testing.T) {
	t.Parallel()
	nest := func(n int) nbt.Tag {
		var tag nbt.Tag = nbt.String("a")
		for range n {
			tag = nbt.Compound{"text": nbt.String("a"), "extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tag}}}
		}
		return tag
	}
	if !RawTextFits(nest(200)) {
		t.Error("200 levels refused")
	}
	if RawTextFits(nest(300)) {
		t.Error("300 levels accepted, the client reads 512 NBT levels")
	}
	if _, ok := RawTextOf(nest(300)); ok {
		t.Error("RawTextOf accepted 300 levels")
	}
}

func TestRawTextEncodingIsDeterministic(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{"text": nbt.String("a"), "bold": nbt.Byte(1), "italic": nbt.Byte(1), "insertion": nbt.String("i")}
	var first Buffer
	first.PutText(Text{Raw: tag})
	for range 20 {
		var b Buffer
		b.PutText(Text{Raw: tag})
		if string(b.Bytes()) != string(first.Bytes()) {
			t.Fatal("two writes of one component differ")
		}
	}
}

func TestStyledTextOfKeepsUnmodelledStyle(t *testing.T) {
	t.Parallel()
	text := func(fields nbt.Compound) nbt.Compound {
		out := nbt.Compound{"text": nbt.String("a")}
		maps.Copy(out, fields)
		return out
	}
	floats := nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(1), nbt.Float(0), nbt.Float(0), nbt.Float(1)}}
	raw := []nbt.Tag{
		text(nbt.Compound{"bold": nbt.Byte(1)}),
		text(nbt.Compound{"italic": nbt.Byte(0)}),
		text(nbt.Compound{"font": nbt.String("minecraft:alt")}),
		text(nbt.Compound{"shadow_color": nbt.Int(-1)}),
		text(nbt.Compound{"shadow_color": floats}),
		text(nbt.Compound{"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{text(nbt.Compound{"strikethrough": nbt.Byte(1)})}}}),
		text(nbt.Compound{"hover_event": nbt.Compound{"action": nbt.String("show_text"), "value": text(nbt.Compound{"obfuscated": nbt.Byte(1)})}}),
	}
	for _, tag := range raw {
		got, ok := StyledTextOf(tag)
		if !ok || got.Raw == nil || !nbt.Equal(got.Raw, tag) {
			t.Errorf("StyledTextOf(%v) = %+v, %v, want the tag kept raw", tag, got, ok)
		}
	}
	refused := []nbt.Tag{
		text(nbt.Compound{"bold": nbt.String("yes")}),
		text(nbt.Compound{"font": nbt.String("Not Valid")}),
		text(nbt.Compound{"shadow_color": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(1)}}}),
		text(nbt.Compound{"bold": nbt.Byte(1), "color": nbt.String("zzz")}),
		nbt.Compound{"bold": nbt.Byte(1)},
	}
	for _, tag := range refused {
		if got, ok := StyledTextOf(tag); ok {
			t.Errorf("StyledTextOf(%v) = %+v, want it refused", tag, got)
		}
	}
	if got, ok := StyledTextOf(text(nbt.Compound{"italic": nbt.Byte(1)})); !ok || got.Raw != nil || !got.Italic {
		t.Errorf("StyledTextOf(italic:1b) = %+v, %v, want the modelled form", got, ok)
	}
	// An unstyled component nested past 32 extra levels is read as TextOf reads it.
	deep := text(nil)
	for range 40 {
		deep = text(nbt.Compound{"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{deep}}})
	}
	want, wantOK := TextOf(deep)
	if got, ok := StyledTextOf(deep); !wantOK || !ok || got.Raw != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("StyledTextOf(40 nested extras) = %v, want TextOf's modelled form (TextOf ok %v)", ok, wantOK)
	}
}

// vanilla: Style.Serializer.MAP_CODEC, KeybindContents
func TestStyledTextOfKeepsEveryStyleKeyAndKeybind(t *testing.T) {
	t.Parallel()
	for name, tag := range map[string]nbt.Tag{
		"bold":          nbt.Compound{"text": nbt.String("x"), "bold": nbt.Byte(1)},
		"strikethrough": nbt.Compound{"text": nbt.String("x"), "strikethrough": nbt.Byte(1)},
		"obfuscated":    nbt.Compound{"text": nbt.String("x"), "obfuscated": nbt.Byte(1)},
		"font":          nbt.Compound{"text": nbt.String("x"), "font": nbt.String("minecraft:uniform")},
		"shadow_color":  nbt.Compound{"text": nbt.String("x"), "shadow_color": nbt.Int(-1)},
		"keybind":       nbt.Compound{"keybind": nbt.String("key.jump")},
		"keybind_bold":  nbt.Compound{"keybind": nbt.String("key.jump"), "bold": nbt.Byte(1), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("y")}}},
	} {
		got, ok := StyledTextOf(tag)
		if !ok || got.Raw == nil || !nbt.Equal(got.Raw, tag) {
			t.Errorf("%s: StyledTextOf = %+v, %v; want the tag kept", name, got, ok)
		}
	}
	if _, ok := StyledTextOf(nbt.Compound{"keybind": nbt.Int(1)}); ok {
		t.Error("a non-string keybind was accepted")
	}
}

// vanilla: Entity.removeAction
func TestWithoutClickDropsTheClickFromARawText(t *testing.T) {
	t.Parallel()
	click := nbt.Compound{"action": nbt.String("copy_to_clipboard"), "value": nbt.String("v")}
	raw := nbt.Compound{"text": nbt.String("x"), "bold": nbt.Byte(1), "click_event": click}
	got := Text{Raw: raw}.WithoutClick()
	want := nbt.Compound{"text": nbt.String("x"), "bold": nbt.Byte(1)}
	if !nbt.Equal(got.Raw, want) {
		t.Errorf("WithoutClick = %v, want %v", got.Raw, want)
	}
	if _, kept := raw["click_event"]; !kept {
		t.Error("the stored tag was edited")
	}
}
