package registry

import (
	"testing"
)

func TestDamageTypeBitsMatchSynchronizedTags(t *testing.T) {
	reg, _ := Get("minecraft:damage_type")
	for id, name := range reg.Entries {
		d, ok := DamageTypeByName(name)
		if !ok || int32(d)-1 != int32(id) || d.Name() != name {
			t.Fatalf("damage registry ID %d: %v, %v", id, d, ok)
		}
		if d.Info() != DamageTypes[name] {
			t.Fatalf("%s: fields differ from generated data", name)
		}
		for _, tags := range Tags {
			if tags.Registry != "minecraft:damage_type" {
				continue
			}
			for _, tag := range tags.Tags {
				want := false
				for _, member := range tag.Entries {
					want = want || member == name
				}
				if got := d.InTag(DamageTypeTagOf(tag.Name)); got != want {
					t.Errorf("%s in %s = %v, want %v", name, tag.Name, got, want)
				}
			}
		}
	}
	for _, d := range []DamageType{0, -1, damageTypeCount, 1 << 30} {
		if d.Name() != "" || d.InTag(DamageTypeTagIDIsFire) {
			t.Errorf("invalid source %d has a name or tags", d)
		}
	}
	if DamageTypeLava.InTag(DamageTypeTagOf("pack:missing")) {
		t.Error("missing tag has members")
	}
}
