package player

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestAttributesLoadNumericTagsAndSaveQualifiedIDs(t *testing.T) {
	t.Parallel()
	for _, number := range []nbt.Tag{nbt.Byte(2), nbt.Short(2), nbt.Int(2), nbt.Long(2), nbt.Float(2.5), nbt.Double(2.5)} {
		t.Run(fmt.Sprintf("%T", number), func(t *testing.T) {
			loaded := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{
					"id": nbt.String("armor"), "base": number,
					"modifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
						nbt.Compound{"id": nbt.String("bonus"), "amount": number, "operation": nbt.String("add_value")},
					}},
				},
			}}
			var a Attributes
			a.Load(loaded)
			want, _ := nbt.NumberValue(number)
			if got := a.Value(registry.AttributeArmor); got != 2*want {
				t.Fatalf("armor = %v, want %v", got, 2*want)
			}
			saved := a.Save(&loaded)
			if saved == nil || len(saved.Items) != 1 {
				t.Fatalf("Save = %v, want one canonical entry", saved)
			}
			entry, _ := nbt.CompoundView(saved.Items[0])
			if entry["id"] != nbt.String("minecraft:armor") || entry["base"] != nbt.Double(want) {
				t.Errorf("saved attribute = %v", entry)
			}
			mods := entry["modifiers"].(nbt.List)
			mod, _ := nbt.CompoundView(mods.Items[0])
			if mod["id"] != nbt.String("minecraft:bonus") || mod["amount"] != nbt.Double(want) {
				t.Errorf("saved modifier = %v", mod)
			}
			var back Attributes
			back.Load(*saved)
			if got := back.Value(registry.AttributeArmor); got != 2*want {
				t.Errorf("reloaded armor = %v, want %v", got, 2*want)
			}
		})
	}
}

// vanilla: AttributeMap.getInstance, pack, apply
func TestSavePlayerIncludesOnlyInstantiatedAttributes(t *testing.T) {
	t.Parallel()
	var a Attributes
	introduced := a.Introduce()
	before := a.SavePlayer(nil)
	if len(before.Items) != len(startingAttributes) {
		t.Fatalf("fresh attributes = %v, want only the starting instances", before)
	}
	a.Value(registry.AttributeArmor)
	a.RemoveModifier(registry.AttributeArmor, "minecraft:absent")
	if got := a.SavePlayer(nil); !reflect.DeepEqual(got, before) {
		t.Errorf("read or absent removal created a saved instance: %v", got)
	}
	a.Instance(registry.AttributeOxygenBonus)
	a.SetBase(registry.AttributeLuck, 0)
	a.SetBase(registry.AttributeArmor, 5)
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:kept", Amount: 2, Persistent: true})
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:transient", Amount: 3})
	loaded := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:attack_speed"), "base": nbt.Double(4)},
	}}
	a.Load(loaded)
	// No loaded list is needed to retain an instance loaded at its default.
	saved := a.SavePlayer(nil)
	if len(saved.Items) != len(startingAttributes)+4 {
		t.Fatalf("saved attributes = %v, want the starting and four instantiated attributes", saved)
	}
	var back Attributes
	back.Load(*saved)
	if got := back.SavePlayer(nil); !reflect.DeepEqual(got, saved) {
		t.Errorf("round trip = %v, want %v", got, saved)
	}
	if back.Base(registry.AttributeArmor) != 5 {
		t.Error("changed base was lost")
	}
	if _, ok := back.Modifier(registry.AttributeArmor, "minecraft:kept"); !ok {
		t.Error("persistent modifier was lost")
	}
	if _, ok := back.Modifier(registry.AttributeArmor, "minecraft:transient"); ok {
		t.Error("transient modifier was saved")
	}
	if got := a.TakeDirty(); len(got) != 1 || got[0].Attribute.ID != int32(registry.AttributeArmor) {
		t.Errorf("dirty = %v, want only the changed armor", got)
	}
	if got := a.SavePlayer(nil); !reflect.DeepEqual(got, saved) {
		t.Errorf("clearing sync changes altered saved participation: %v", got)
	}
	if len(introduced) != len(startingAttributes) {
		t.Errorf("introduced attributes = %v, want the starting instances", introduced)
	}
}
