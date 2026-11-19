package protocol

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// The minecraft:profile component's two directions: ProfileToTag reads the
// wire value a client sends into the compound a skull block entity keeps, and
// ProfileFromTag writes that compound back for the drop.

// profileProperty is one entry of a profile's property list as the tests
// build it: the name and value the wire always carries, and a signature the
// wire adds only when hasSig is set.
type profileProperty struct {
	name, value, signature string
	hasSig                 bool
}

// profileWire writes a minecraft:profile value in the complete shape: the
// UUID and name together, then the property list and an empty skin patch.
func profileWire(id uuid.UUID, name string, properties ...profileProperty) []byte {
	var b Buffer
	b.PutBool(true)
	b.PutUUID(id)
	b.PutString(name)
	b.PutVarInt(int32(len(properties)))
	for _, property := range properties {
		b.PutString(property.name)
		b.PutString(property.value)
		b.PutBool(property.hasSig)
		if property.hasSig {
			b.PutString(property.signature)
		}
	}
	b.PutBool(false) // body texture
	b.PutBool(false) // cape texture
	b.PutBool(false) // elytra texture
	b.PutBool(false) // slim model
	return b.Bytes()
}

// partialProfileWire writes a profile value in one of the partial shapes: a
// name alone, a UUID alone, or neither, followed by an empty property list
// and an empty skin patch.
func partialProfileWire(hasID bool, id uuid.UUID, hasName bool, name string) []byte {
	var b Buffer
	b.PutBool(false)
	b.PutBool(hasName)
	if hasName {
		b.PutString(name)
	}
	b.PutBool(hasID)
	if hasID {
		b.PutUUID(id)
	}
	b.PutVarInt(0)
	b.PutBool(false) // body texture
	b.PutBool(false) // cape texture
	b.PutBool(false) // elytra texture
	b.PutBool(false) // slim model
	return b.Bytes()
}

// repeatProperties is a property list of n entries, each value distinct
// enough to keep the list from being deduplicated by a reader.
func repeatProperties(n int) []profileProperty {
	properties := make([]profileProperty, n)
	for i := range properties {
		properties[i] = profileProperty{name: "n", value: strings.Repeat("v", i+1)}
	}
	return properties
}

// namedCompound builds a profile compound from a name and a property list,
// the two fields ProfileFromTag reads its bounds from. An empty name or a
// nil list leaves those fields out.
func namedCompound(name string, properties []nbt.Tag) nbt.Compound {
	tag := nbt.Compound{}
	if name != "" {
		tag["name"] = nbt.String(name)
	}
	if properties != nil {
		tag["properties"] = nbt.List{Elem: nbt.TypeCompound, Items: properties}
	}
	return tag
}

// propertyTag is one compound property entry the block entity keeps.
func propertyTag(name, value string) nbt.Tag {
	return nbt.Compound{"name": nbt.String(name), "value": nbt.String(value)}
}

// TestProfileTagRoundTripsTheCompleteShape writes a profile with a UUID, a
// name and a property the wire carries a signature for, reads it into the
// compound a block entity keeps, and writes it back: the bytes are the value
// the wire started with.
func TestProfileTagRoundTripsTheCompleteShape(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	wire := profileWire(id, "Notch",
		profileProperty{name: "textures", value: "abc", signature: "sig", hasSig: true},
		profileProperty{name: "other", value: "def"},
	)
	tag, ok := ProfileToTag(wire)
	if !ok {
		t.Fatal("ProfileToTag refused a complete profile")
	}
	if got, ok := nbt.Get[nbt.IntArray](tag, "id"); !ok || !slices.Equal(got, nbt.UUIDToIntArray(id)) {
		t.Errorf("the compound's id = %v, want %v", got, nbt.UUIDToIntArray(id))
	}
	if got, _ := nbt.Get[nbt.String](tag, "name"); string(got) != "Notch" {
		t.Errorf("the compound's name = %q, want Notch", got)
	}
	if _, ok := nbt.Get[nbt.List](tag, "properties"); !ok {
		t.Error("the compound kept no property list")
	}
	back, ok := ProfileFromTag(tag)
	if !ok {
		t.Fatal("ProfileFromTag refused the compound it read")
	}
	if !bytes.Equal(back, wire) {
		t.Errorf("the written profile = %x, want the wire's %x", back, wire)
	}
}

// TestProfileTagRoundTripsTheSkinPatch reads a profile whose wire carries
// two of the three skin textures and a slim model, checks the compound keeps
// each under its own field and leaves the absent cape out, and writes it back
// to the same bytes.
func TestProfileTagRoundTripsTheSkinPatch(t *testing.T) {
	id := uuid.MustParse("12121212-3434-5656-7878-909090909090")
	var b Buffer
	b.PutBool(true)
	b.PutUUID(id)
	b.PutString("Notch")
	b.PutVarInt(0)
	b.PutBool(true) // body texture
	b.PutString("minecraft:entity/player/wide/steve")
	b.PutBool(false) // no cape
	b.PutBool(true)  // elytra texture
	b.PutString("minecraft:entity/player/wide/alex_elytra")
	b.PutBool(true) // a model is carried
	b.PutBool(true) // the slim model
	wire := b.Bytes()

	tag, ok := ProfileToTag(wire)
	if !ok {
		t.Fatal("ProfileToTag refused a profile carrying a skin patch")
	}
	if got, _ := nbt.Get[nbt.String](tag, "texture"); string(got) != "minecraft:entity/player/wide/steve" {
		t.Errorf("the compound's texture = %q, want minecraft:entity/player/wide/steve", got)
	}
	if _, ok := nbt.Get[nbt.String](tag, "cape"); ok {
		t.Error("the compound kept a cape the wire left out")
	}
	if got, _ := nbt.Get[nbt.String](tag, "elytra"); string(got) != "minecraft:entity/player/wide/alex_elytra" {
		t.Errorf("the compound's elytra = %q, want minecraft:entity/player/wide/alex_elytra", got)
	}
	if got, _ := nbt.Get[nbt.String](tag, "model"); string(got) != "slim" {
		t.Errorf("the compound's model = %q, want slim", got)
	}
	back, ok := ProfileFromTag(tag)
	if !ok {
		t.Fatal("ProfileFromTag refused the compound it read")
	}
	if !bytes.Equal(back, wire) {
		t.Errorf("the written profile = %x, want the wire's %x", back, wire)
	}
}

// TestProfileToTagRefusesValuesPastTheLimits reads values with one field past
// the bound the wire codec reads back: each is refused whole, since a client
// would drop the packet a longer one came in on.
func TestProfileToTagRefusesValuesPastTheLimits(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	cases := []struct {
		name string
		wire []byte
	}{
		{"name past 16 units", profileWire(id, strings.Repeat("a", 17))},
		{"17 properties", profileWire(id, "Notch", repeatProperties(17)...)},
		{"property name past 64 units",
			profileWire(id, "Notch", profileProperty{name: strings.Repeat("a", 65), value: "v"})},
		{"property value past 32767 units",
			profileWire(id, "Notch", profileProperty{name: "n", value: strings.Repeat("a", 32768)})},
		{"signature past 1024 units",
			profileWire(id, "Notch", profileProperty{name: "n", value: "v", signature: strings.Repeat("a", 1025), hasSig: true})},
		{"truncated", profileWire(id, "Notch")[:5]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tag, ok := ProfileToTag(tc.wire); ok {
				t.Errorf("ProfileToTag = %v, true, want the value refused", tag)
			}
		})
	}
}

// TestProfileFromTagRefusesValuesPastTheLimits writes compounds with one
// field past the bound the wire codec reads back: each is refused whole.
func TestProfileFromTagRefusesValuesPastTheLimits(t *testing.T) {
	cases := []struct {
		name string
		tag  nbt.Compound
	}{
		{"name past 16 units", namedCompound(strings.Repeat("a", 17), nil)},
		{"17 properties", namedCompound("Notch", repeatPropertyTags(17))},
		{"property name past 64 units",
			namedCompound("Notch", []nbt.Tag{propertyTag(strings.Repeat("a", 65), "v")})},
		{"property value past 32767 units",
			namedCompound("Notch", []nbt.Tag{propertyTag("n", strings.Repeat("a", 32768))})},
		{"signature past 1024 units",
			namedCompound("Notch", []nbt.Tag{nbt.Compound{
				"name":      nbt.String("n"),
				"value":     nbt.String("v"),
				"signature": nbt.String(strings.Repeat("a", 1025)),
			}})},
		{"texture that is no identifier",
			nbt.Compound{"name": nbt.String("Notch"), "texture": nbt.String("Not An Identifier")}},
		{"model the codec does not name",
			nbt.Compound{"name": nbt.String("Notch"), "model": nbt.String("broad")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if raw, ok := ProfileFromTag(tc.tag); ok {
				t.Errorf("ProfileFromTag = %x, true, want the compound refused", raw)
			}
		})
	}
}

// repeatPropertyTags is a property list of n well-formed entries.
func repeatPropertyTags(n int) []nbt.Tag {
	items := make([]nbt.Tag, n)
	for i := range items {
		items[i] = propertyTag("n", "v")
	}
	return items
}

// TestProfileFromTagReadsTheMapShapes writes the stored shapes a vanilla
// skull keeps: the id as a list of ints, and the properties as one map of
// name to value list. Each writes the wire value the codec reads back.
// vanilla: UUIDUtil.CODEC, ExtraCodecs.PROPERTY_MAP
func TestProfileFromTagReadsTheMapShapes(t *testing.T) {
	t.Run("id as a list of ints", func(t *testing.T) {
		tag := nbt.Compound{"id": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{
			nbt.Int(0x11111111), nbt.Int(0x22223333), nbt.Int(0x44445555), nbt.Int(0x66667777),
		}}}
		back, ok := ProfileFromTag(tag)
		if !ok {
			t.Fatal("ProfileFromTag refused an id written as a list of ints")
		}
		id := uuid.MustParse("11111111-2222-3333-4444-555566667777")
		if want := partialProfileWire(true, id, false, ""); !bytes.Equal(back, want) {
			t.Errorf("the written profile = %x, want %x", back, want)
		}
	})

	t.Run("properties as a map", func(t *testing.T) {
		tag := nbt.Compound{
			"name": nbt.String("Notch"),
			"properties": nbt.Compound{
				"textures": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
					nbt.String("abc"), nbt.String("def"),
				}},
			},
		}
		back, ok := ProfileFromTag(tag)
		if !ok {
			t.Fatal("ProfileFromTag refused properties written as a map")
		}
		var want Buffer
		want.PutBool(false)
		want.PutBool(true)
		want.PutString("Notch")
		want.PutBool(false)
		want.PutVarInt(2)
		want.PutString("textures")
		want.PutString("abc")
		want.PutBool(false)
		want.PutString("textures")
		want.PutString("def")
		want.PutBool(false)
		want.PutBool(false)
		want.PutBool(false)
		want.PutBool(false)
		want.PutBool(false)
		if !bytes.Equal(back, want.Bytes()) {
			t.Errorf("the written profile = %x, want %x", back, want.Bytes())
		}
	})
}
