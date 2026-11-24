package player

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
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
	checkSavedPlayerAttributeOrder(t, saved, "movement_speed entity_interaction_range armor luck oxygen_bonus block_interaction_range attack_speed")
	var back Attributes
	back.Load(*saved)
	// Oxygen bonus and block reach share a bucket. Loading the packed list
	// instantiates oxygen bonus first, reversing their traversal order.
	checkSavedPlayerAttributeOrder(t, back.SavePlayer(saved), "movement_speed entity_interaction_range armor luck block_interaction_range oxygen_bonus attack_speed")
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

func TestSavePlayerAttributeOrderAfterGrowth(t *testing.T) {
	t.Parallel()
	// The 25th instance grows the table. Vanilla reinserts from the old
	// table's high buckets down, which matters for collisions after growth.
	loaded := nbt.List{Elem: nbt.TypeCompound}
	for id := range strings.FieldsSeq(`
		air_drag_modifier armor armor_toughness attack_damage attack_knockback attack_speed below_name_distance
		block_break_speed block_interaction_range bounciness burning_time camera_distance explosion_knockback_resistance
		entity_interaction_range fall_damage_multiplier friction_modifier gravity jump_strength knockback_resistance
		luck max_absorption max_health mining_efficiency movement_efficiency movement_speed`) {
		loaded.Items = append(loaded.Items, AttributeTag(registry.Qualify(id), 0, nil))
	}
	loaded.Items = append(loaded.Items, AttributeTag("minecraft:armor", 6, nil), AttributeTag("other:attribute", 2, nil))
	var a Attributes
	a.Load(loaded)
	checkSavedPlayerAttributeOrder(t, a.SavePlayer(&loaded), `
		burning_time max_absorption entity_interaction_range friction_modifier below_name_distance bounciness gravity
		max_health block_interaction_range explosion_knockback_resistance attack_speed armor_toughness attack_knockback
		movement_speed attack_damage armor block_break_speed knockback_resistance air_drag_modifier jump_strength luck
		camera_distance mining_efficiency fall_damage_multiplier movement_efficiency other:attribute`)
	if got := a.Base(registry.AttributeArmor); got != 6 {
		t.Errorf("armor after applying duplicate entries = %v, want 6", got)
	}
}

func checkSavedPlayerAttributeOrder(t *testing.T, list *nbt.List, names string) {
	t.Helper()
	want := strings.Fields(names)
	for i, name := range want {
		want[i] = registry.Qualify(name)
	}
	var got []string
	for _, item := range list.Items {
		tag, _ := nbt.CompoundView(item)
		id, _ := nbt.Get[nbt.String](tag, "id")
		got = append(got, string(id))
	}
	if !slices.Equal(got, want) {
		t.Errorf("attribute order = %v, want %v", got, want)
	}
}
