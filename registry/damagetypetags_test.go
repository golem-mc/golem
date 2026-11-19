package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
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

func TestDamageTypeBitsReloadCustomTypesAndTags(t *testing.T) {
	t.Cleanup(func() { ApplyPackDamageTypes(nil); ApplyPackRegistries(nil); ApplyPackTags(nil) })
	ApplyPackDamageTypes(map[string]DamageTypeInfo{"pack:zap": {MessageID: "zap", Exhaustion: 0.5}, "minecraft:lava": {MessageID: "molten"}})
	if problems := ApplyPackRegistries(map[string]map[string]nbt.Tag{"minecraft:damage_type": {"pack:zap": nbt.Compound{}}}); len(problems) != 0 {
		t.Fatal(problems)
	}
	packs := PackTags{"minecraft:damage_type": {
		"minecraft:is_fire": {Replace: true, Entries: []PackTagEntry{{ID: "pack:zap", Required: true}}},
		"pack:custom":       {Entries: []PackTagEntry{{ID: "minecraft:is_fire", Tag: true, Required: true}}},
	}}
	if problems := ApplyPackTags(packs); len(problems) != 0 {
		t.Fatal(problems)
	}
	zap, ok := DamageTypeByName("pack:zap")
	reg, _ := Get("minecraft:damage_type")
	if !ok || int32(zap)-1 != reg.ID("pack:zap") || zap.Info().Exhaustion != 0.5 {
		t.Fatalf("custom damage ID or fields: %v, %v", zap, ok)
	}
	if !zap.InTag(DamageTypeTagIDIsFire) || !zap.InTag(DamageTypeTagOf("pack:custom")) || DamageTypeLava.InTag(DamageTypeTagIDIsFire) {
		t.Fatal("replacement and nested custom tags did not apply")
	}
	if DamageTypeLava.Info().MessageID != "molten" {
		t.Fatal("built-in damage field override lost")
	}
	ApplyPackTags(nil)
	if zap.InTag(DamageTypeTagIDIsFire) || !DamageTypeLava.InTag(DamageTypeTagIDIsFire) || DamageTypeTagExists("pack:custom") {
		t.Fatal("tag reset retained pack membership")
	}
	// A failed built-in tag keeps its constant but has no members.
	ApplyPackTags(PackTags{"minecraft:damage_type": {"minecraft:is_fire": {Replace: true, Entries: []PackTagEntry{{ID: "pack:missing", Required: true}}}}})
	if DamageTypeTagExists("minecraft:is_fire") || DamageTypeLava.InTag(DamageTypeTagIDIsFire) {
		t.Fatal("failed built-in tag was retained")
	}
}
