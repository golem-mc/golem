package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestEntityFilesFromBefore1_21_5GetComponentFixes loads a zombie and an armor
// stand saved at 3955 and checks the names, attributes and held stack.
// vanilla: AttributeIdPrefixFix, UnflattenTextComponentFix
func TestEntityFilesFromBefore1_21_5GetComponentFixes(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	sword := nbt.Compound{
		"id": nbt.String("minecraft:diamond_sword"), "count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:custom_name": nbt.String(`{"text":"Edge","color":"red"}`),
			"minecraft:attribute_modifiers": nbt.Compound{"modifiers": list(nbt.Compound{
				"type": nbt.String("minecraft:generic.attack_damage"), "id": nbt.String("minecraft:x"),
				"amount": nbt.Double(3), "operation": nbt.String("add_value"), "slot": nbt.String("mainhand"),
			})},
		},
	}
	zombie := nbt.Compound{
		"id":         nbt.String("minecraft:zombie"),
		"CustomName": nbt.String(`{"text":"Bob","color":"gold"}`),
		"Health":     nbt.Float(30),
		"attributes": list(
			nbt.Compound{"id": nbt.String("minecraft:generic.max_health"), "base": nbt.Double(40)},
			nbt.Compound{"id": nbt.String("minecraft:generic.movement_speed"), "base": nbt.Double(0.3)},
		),
		"HandItems": list(sword, nbt.Compound{}),
	}
	stand := nbt.Compound{"id": nbt.String("minecraft:armor_stand"), "CustomName": nbt.String(`"Stand"`)}
	pos := ChunkPos{}
	plantEntityRoot(t, w, pos, nbt.Compound{"DataVersion": nbt.Int(3955), "Entities": list(zombie, stand)})

	got, _, err := w.LoadChunkEntities(pos)
	if err != nil || len(got) != 2 {
		t.Fatalf("entities = %v, %v", got, err)
	}
	z, _ := nbt.CompoundView(got[0])
	name, ok := nbt.CompoundView(z["CustomName"])
	if !ok || name["text"] != nbt.String("Bob") || name["color"] != nbt.String("gold") {
		t.Errorf("zombie CustomName = %v, want Bob in gold", z["CustomName"])
	}
	attrs, _ := z["attributes"].(nbt.List)
	for i, want := range []string{"minecraft:max_health", "minecraft:movement_speed"} {
		a, _ := nbt.CompoundView(attrs.Items[i])
		if a["id"] != nbt.String(want) {
			t.Errorf("attribute %d id = %v, want %s", i, a["id"], want)
		}
	}
	equipment, _ := nbt.CompoundView(z["equipment"])
	held, _ := nbt.CompoundView(equipment["mainhand"])
	comps, _ := nbt.CompoundView(held["components"])
	edge, _ := nbt.CompoundView(comps["minecraft:custom_name"])
	if edge["text"] != nbt.String("Edge") {
		t.Errorf("sword name = %v, want Edge", comps["minecraft:custom_name"])
	}
	ml, _ := comps["minecraft:attribute_modifiers"].(nbt.List)
	m, _ := nbt.CompoundView(ml.Items[0])
	if m["type"] != nbt.String("minecraft:attack_damage") {
		t.Errorf("modifier type = %v, want minecraft:attack_damage", m["type"])
	}
	s, _ := nbt.CompoundView(got[1])
	if s["CustomName"] != nbt.String("Stand") {
		t.Errorf("armor stand CustomName = %v, want Stand", s["CustomName"])
	}
}
