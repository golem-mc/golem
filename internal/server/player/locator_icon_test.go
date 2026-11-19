package player

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestLocatorIconTagRoundTrip(t *testing.T) {
	in := LocatorIcon{Style: "foo:bar", Color: -1<<24 | 0x33AA77, HasColor: true}
	if out := LocatorIconFromTag(in.Tag()); out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
	if (LocatorIcon{}).HasData() || !(LocatorIcon{Style: "foo:bar"}).HasData() {
		t.Error("HasData must be false for the default icon and true for a changed one")
	}
	floats := nbt.Compound{"style": nbt.String("minecraft:default"), "color": nbt.List{Elem: nbt.TypeFloat,
		Items: []nbt.Tag{nbt.Float(1), nbt.Float(0), nbt.Float(0.5)}}}
	if got := LocatorIconFromTag(floats); !got.HasColor || got.Color&0xFFFFFF != 0xFF007F {
		t.Errorf("float list colour = %#x, want ff007f", got.Color)
	}
}
