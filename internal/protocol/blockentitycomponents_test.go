package protocol

import (
	"bytes"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// TestSetProfileRefusesAnOversizedProfile checks the codec limits SetProfile
// enforces by reading its own value back: a name past the sixteen code units
// ExtraCodecs.PLAYER_NAME allows, and a property list past the sixteen
// ResolvableProfile carries.
func TestSetProfileRefusesAnOversizedProfile(t *testing.T) {
	base := Profile{UUID: uuid.Nil, Name: "Writer", HasUUID: true, HasName: true}

	longName := base
	longName.Name = "WriterNameTooLong"
	if got := (&ItemStack{}).SetProfile(longName); got {
		t.Error("SetProfile accepted a name over sixteen characters")
	}

	properties := make([]ProfileProperty, 17)
	for i := range properties {
		properties[i] = ProfileProperty{Name: "textures", Value: "value"}
	}
	tooMany := base
	tooMany.Properties = properties
	if got := (&ItemStack{}).SetProfile(tooMany); got {
		t.Error("SetProfile accepted more than sixteen properties")
	}
}

// TestPotDecorationsAreFourOptionalTemplates pins the component's shape: a
// side is a flag and, when set, an item template; a bare side is the flag
// alone, and a list of item IDs is not read.
// vanilla: PotDecorations.STREAM_CODEC
func TestPotDecorationsAreFourOptionalTemplates(t *testing.T) {
	var pot ItemStack
	sides := [4]ItemStack{{ItemID: 7, Count: 1}, {}, {ItemID: 9, Count: 1}, {}}
	if !pot.SetPotDecorations(sides) {
		t.Fatal("SetPotDecorations refused two items and two bare sides")
	}
	// back: present, item 7, count 1, no components; left: absent; right
	// the same with item 9; front: absent.
	want := []byte{0x01, 0x07, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x00, 0x00, 0x00}
	if len(pot.Raw) != 1 || pot.Raw[0].Type != DataComponentPotDecorations || !bytes.Equal(pot.Raw[0].Data, want) {
		t.Fatalf("SetPotDecorations laid %+v, want the value % X", pot.Raw, want)
	}
	if got, ok := pot.PotDecorationsOf(); !ok || !slices.EqualFunc(got[:], sides[:], ItemStack.Equal) {
		t.Errorf("PotDecorationsOf = %v (%v), want %v", got, ok, sides)
	}
	got, ok := NewReader(untrustedSlotOf(1, DataComponentPotDecorations, want)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the decorations")
	}
	if read, ok := got.PotDecorationsOf(); !ok || !slices.EqualFunc(read[:], sides[:], ItemStack.Equal) {
		t.Errorf("the read stack's decorations = %v (%v), want %v", read, ok, sides)
	}

	for name, value := range map[string][]byte{
		"a list of four item IDs":    {0x04, 0x07, 0x07, 0x07, 0x07},
		"three sides":                {0x00, 0x00, 0x00},
		"a side of no items":         {0x01, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		"a side whose item is air":   {0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
		"a template that ends early": {0x00, 0x00, 0x00, 0x01, 0x07},
	} {
		if stack, ok := NewReader(untrustedSlotOf(1, DataComponentPotDecorations, value)).ReadUntrustedSlot(); ok {
			t.Errorf("%s: ReadUntrustedSlot = %+v, want the stack refused", name, stack)
		}
	}
	if (&ItemStack{}).SetPotDecorations([4]ItemStack{{ItemID: RegistrySizeItem, Count: 1}}) {
		t.Error("SetPotDecorations accepted an item past the registry")
	}
}
