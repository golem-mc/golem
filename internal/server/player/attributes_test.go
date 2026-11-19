package player

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestAttributeValueOrdersTheOperations(t *testing.T) {
	var a Attributes
	a.AddModifier(registry.AttributeMovementSpeed, Modifier{ID: "minecraft:total", Amount: 1, Operation: registry.AttributeAddMultipliedTotal})
	a.AddModifier(registry.AttributeMovementSpeed, Modifier{ID: "minecraft:add", Amount: 0.1, Operation: registry.AttributeAddValue})
	a.AddModifier(registry.AttributeMovementSpeed, Modifier{ID: "minecraft:base", Amount: 0.5, Operation: registry.AttributeAddMultipliedBase})
	// (0.1 + 0.1) * 1.5 * 2
	base := float64(float32(0.1))
	if got, want := a.Value(registry.AttributeMovementSpeed), (base+0.1)*1.5*2; got != want {
		t.Errorf("speed = %v, want %v", got, want)
	}
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:lots", Amount: 100})
	if got := a.Value(registry.AttributeArmor); got != 30 {
		t.Errorf("armour = %v, want the cap of 30", got)
	}
}

// TestReadingAnAttributeMakesNoInstance pins that Value and RemoveModifier
// leave the map as it was: an unmodified syncable attribute nobody has a
// modifier on is not introduced to the client at a join.
func TestReadingAnAttributeMakesNoInstance(t *testing.T) {
	var a Attributes
	a.Value(registry.AttributeSafeFallDistance)
	if a.RemoveModifier(registry.AttributeSafeFallDistance, "minecraft:effect.jump_boost") {
		t.Error("a modifier that was never there was reported removed")
	}
	all := a.Introduce()
	if len(all) != len(startingAttributes) {
		t.Errorf("introduced %d attributes, want %d", len(all), len(startingAttributes))
	}
	for _, in := range all {
		if in.Attribute.Name == "minecraft:safe_fall_distance" {
			t.Error("safe_fall_distance was introduced with no modifier on it")
		}
	}
}

func TestAttributesReportChangesOnceIntroduced(t *testing.T) {
	var a Attributes
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:armor.chestplate", Amount: 6})
	if got := a.TakeDirty(); got != nil {
		t.Errorf("changes before the introduction = %+v, want none", got)
	}
	all := a.Introduce()
	// The map holds the three starts plus the armour the modifier made.
	if len(all) != len(startingAttributes)+1 {
		t.Errorf("introduced %d attributes, want %d", len(all), len(startingAttributes)+1)
	}
	if all[0].Attribute.Name != "minecraft:armor" || len(all[0].Modifiers) != 1 {
		t.Errorf("first introduced = %+v, want the armour with its modifier", all[0])
	}

	// The same modifier again changes nothing; a non-syncable attribute
	// changes nothing the client hears of.
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:armor.chestplate", Amount: 6})
	a.AddModifier(registry.AttributeKnockbackResistance, Modifier{ID: "minecraft:armor.chestplate", Amount: 0.1})
	if got := a.TakeDirty(); len(got) != 0 {
		t.Errorf("changes = %+v, want none", got)
	}
	if !a.RemoveModifier(registry.AttributeArmor, "minecraft:armor.chestplate") {
		t.Fatal("the chestplate modifier was not there to remove")
	}
	if a.RemoveModifier(registry.AttributeArmor, "minecraft:armor.chestplate") {
		t.Error("removed the chestplate modifier twice")
	}
	got := a.TakeDirty()
	if len(got) != 1 || got[0].Attribute.Name != "minecraft:armor" || len(got[0].Modifiers) != 0 {
		t.Errorf("changes = %+v, want the bare armour", got)
	}
	if got := a.TakeDirty(); len(got) != 0 {
		t.Errorf("changes taken twice: %+v", got)
	}
}

// TestAttributesSaveRoundTrip saves a changed base and a persistent modifier
// and loads them onto a fresh map, leaving the transient modifier behind.
func TestAttributesSaveRoundTrip(t *testing.T) {
	var a Attributes
	a.SetBase(registry.AttributeArmor, 5)
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:t", Amount: 2, Operation: registry.AttributeAddMultipliedBase, Persistent: true})
	a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:worn", Amount: 3})
	list := a.Save(nil)
	if list == nil || len(list.Items) != 1 {
		t.Fatalf("Save = %v, want one entry", list)
	}
	var b Attributes
	b.Load(*list)
	if got := b.Base(registry.AttributeArmor); got != 5 {
		t.Errorf("base = %v, want 5", got)
	}
	if m, ok := b.Modifier(registry.AttributeArmor, "minecraft:t"); !ok || m.Amount != 2 || m.Operation != registry.AttributeAddMultipliedBase {
		t.Errorf("modifier = %+v %v", m, ok)
	}
	if _, ok := b.Modifier(registry.AttributeArmor, "minecraft:worn"); ok {
		t.Error("transient modifier was saved")
	}
	var fresh Attributes
	if fresh.Save(nil) != nil {
		t.Error("untouched map saved something")
	}
}

// TestSaveKeepsLoadedEntries checks a loaded list survives a save: entries
// for attributes the map does not hold stay, and one the map holds is written
// from the map even at its default base.
func TestSaveKeepsLoadedEntries(t *testing.T) {
	loaded := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:not_an_attribute"), "base": nbt.Double(3)},
		nbt.Compound{"id": nbt.String("minecraft:armor"), "base": nbt.Double(0)},
	}}
	var a Attributes
	a.Load(loaded)
	list := a.Save(&loaded)
	if list == nil || !nbt.Equal(*list, loaded) {
		t.Errorf("Save = %v, want %v", list, loaded)
	}
	a.SetBase(registry.AttributeArmor, 4)
	list = a.Save(&loaded)
	entry, _ := nbt.CompoundView(list.Items[1])
	if got := entry["base"]; got != nbt.Double(4) {
		t.Errorf("armor base saved as %v, want 4", got)
	}
}

// TestResetAttributesKeepsBases checks a new body keeps the base values and
// loses the modifiers.
// vanilla: ServerPlayer.restoreFrom
func TestResetAttributesKeepsBases(t *testing.T) {
	p := &Player{}
	p.Attributes(func(a *Attributes) {
		a.SetBase(registry.AttributeArmor, 5)
		a.AddModifier(registry.AttributeArmor, Modifier{ID: "minecraft:t", Amount: 2, Persistent: true})
	})
	p.ResetAttributes()
	p.Attributes(func(a *Attributes) {
		if got := a.Base(registry.AttributeArmor); got != 5 {
			t.Errorf("base = %v, want 5", got)
		}
		if _, ok := a.Modifier(registry.AttributeArmor, "minecraft:t"); ok {
			t.Error("modifier survived the reset")
		}
	})
}

// TestAttributeSetHoldsEveryAttribute pins that attributeSet, one bit per
// attribute ID, is wide enough for the registry.
func TestAttributeSetHoldsEveryAttribute(t *testing.T) {
	if registry.NumAttributes > 64 {
		t.Fatalf("%d attributes do not fit the uint64 attributeSet", registry.NumAttributes)
	}
}
