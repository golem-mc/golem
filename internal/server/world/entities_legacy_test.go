package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestEntityFilesFrom1_20_4GetItemAndFieldFixes loads the entities of a 1.20.4
// record (3700) and checks each against what the 26.3 data fixers make of it.
// vanilla: BlockPosFormatAndRenamesFix, ItemStackComponentizationFix,
// AreaEffectCloudPotionFix, AttributeModifierIdFix
func TestEntityFilesFrom1_20_4GetItemAndFieldFixes(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	stack := func(id string, count int8) nbt.Compound {
		return nbt.Compound{"id": nbt.String(id), "Count": nbt.Byte(count)}
	}
	sword := stack("minecraft:diamond_sword", 1)
	sword["tag"] = nbt.Compound{
		"Enchantments": list(nbt.Compound{"id": nbt.String("minecraft:sharpness"), "lvl": nbt.Short(3)}),
		"Damage":       nbt.Int(5),
	}
	zombie := nbt.Compound{
		"id":         nbt.String("minecraft:zombie"),
		"Health":     nbt.Float(30),
		"HandItems":  list(sword, nbt.Compound{}),
		"ArmorItems": list(nbt.Compound{}, nbt.Compound{}, nbt.Compound{}, stack("minecraft:leather_helmet", 1)),
		"Attributes": list(nbt.Compound{"Name": nbt.String("minecraft:generic.max_health"), "Base": nbt.Double(40)}),
	}
	frame := nbt.Compound{"id": nbt.String("minecraft:item_frame"), "Item": stack("minecraft:diamond", 1)}
	item := nbt.Compound{"id": nbt.String("minecraft:item"), "Item": stack("minecraft:firework_rocket", 2)}
	piglin := nbt.Compound{"id": nbt.String("minecraft:piglin"), "Inventory": list(stack("minecraft:gold_ingot", 3))}
	cloud := nbt.Compound{
		"id": nbt.String("minecraft:area_effect_cloud"), "Potion": nbt.String("minecraft:poison"), "Color": nbt.Int(65280),
	}
	crystal := nbt.Compound{
		"id":         nbt.String("minecraft:end_crystal"),
		"BeamTarget": nbt.Compound{"X": nbt.Int(1), "Y": nbt.Int(2), "Z": nbt.Int(3)},
	}
	pos := ChunkPos{}
	plantEntityRoot(t, w, pos, nbt.Compound{
		"DataVersion": nbt.Int(3700),
		"Entities":    list(zombie, frame, item, piglin, cloud, crystal),
	})
	got, _, err := w.LoadChunkEntities(pos)
	if err != nil || len(got) != 6 {
		t.Fatalf("entities = %v, %v", got, err)
	}
	view := func(tag nbt.Tag) nbt.Compound {
		c, ok := nbt.CompoundView(tag)
		if !ok {
			t.Fatalf("%v is not a compound", tag)
		}
		return c
	}
	z := view(got[0])
	equipment := view(z["equipment"])
	held := view(equipment["mainhand"])
	comps := view(held["components"])
	if held["id"] != nbt.String("minecraft:diamond_sword") || held["count"] != nbt.Int(1) ||
		comps["minecraft:damage"] != nbt.Int(5) {
		t.Errorf("mainhand = %v", held)
	}
	if view(comps["minecraft:enchantments"])["minecraft:sharpness"] != nbt.Int(3) {
		t.Errorf("enchantments = %v", comps["minecraft:enchantments"])
	}
	if view(equipment["head"])["id"] != nbt.String("minecraft:leather_helmet") {
		t.Errorf("head = %v", equipment["head"])
	}
	attrs, _ := z["attributes"].(nbt.List)
	if len(attrs.Items) != 1 || view(attrs.Items[0])["id"] != nbt.String("minecraft:max_health") ||
		view(attrs.Items[0])["base"] != nbt.Double(40) {
		t.Errorf("attributes = %v", z["attributes"])
	}
	if _, ok := z["Attributes"]; ok || z["Health"] != nbt.Float(30) {
		t.Errorf("zombie = %v", z)
	}
	if f := view(view(got[1])["Item"]); f["id"] != nbt.String("minecraft:diamond") || f["count"] != nbt.Int(1) {
		t.Errorf("frame Item = %v", f)
	}
	if i := view(view(got[2])["Item"]); i["id"] != nbt.String("minecraft:firework_rocket") || i["count"] != nbt.Int(2) {
		t.Errorf("item entity Item = %v", i)
	}
	inv, _ := view(got[3])["Inventory"].(nbt.List)
	if g := view(inv.Items[0]); g["id"] != nbt.String("minecraft:gold_ingot") || g["count"] != nbt.Int(3) {
		t.Errorf("piglin Inventory = %v", inv)
	}
	c := view(got[4])
	pc := view(c["potion_contents"])
	if pc["potion"] != nbt.String("minecraft:poison") || pc["custom_color"] != nbt.Int(65280) {
		t.Errorf("cloud potion_contents = %v", pc)
	}
	if _, ok := c["Potion"]; ok {
		t.Errorf("cloud kept Potion: %v", c)
	}
	beam, _ := view(got[5])["beam_target"].(nbt.IntArray)
	if len(beam) != 3 || beam[0] != 1 || beam[1] != 2 || beam[2] != 3 {
		t.Errorf("beam_target = %v", view(got[5]))
	}
}
