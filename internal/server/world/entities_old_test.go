package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestOldEntityFixes(t *testing.T) {
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	entity := func(id string, kv ...any) nbt.Compound {
		c := nbt.Compound{"id": nbt.String(id)}
		for i := 0; i < len(kv); i += 2 {
			c[kv[i].(string)] = kv[i+1].(nbt.Tag)
		}
		return c
	}
	run := func(version int32, e nbt.Compound) nbt.Compound {
		upgradeEntities(list(e), version)
		return e
	}

	if a := run(2586, entity("minecraft:arrow", "player", nbt.Byte(0))); a["pickup"] != nbt.Byte(0) || a["player"] != nil {
		t.Errorf("arrow pickup: %v", a)
	}
	if a := run(2586, entity("minecraft:trident")); a["pickup"] != nbt.Byte(1) {
		t.Errorf("trident pickup: %v", a)
	}
	if c := run(3000, entity("minecraft:cat", "CatType", nbt.Int(4))); c["variant"] != nbt.String("minecraft:british_shorthair") {
		t.Errorf("cat variant: %v", c)
	}
	if f := run(3086, entity("minecraft:frog", "Variant", nbt.Int(2))); f["variant"] != nbt.String("minecraft:cold") {
		t.Errorf("frog variant: %v", f)
	}
	p := run(2586, entity("minecraft:painting", "Motive", nbt.String("minecraft:kebab"), "Facing", nbt.Byte(2)))
	if p["variant"] != nbt.String("minecraft:kebab") || p["facing"] != nbt.Byte(2) || p["Motive"] != nil {
		t.Errorf("painting: %v", p)
	}
	if g := run(3000, entity("minecraft:goat")); g["HasLeftHorn"] != nbt.Byte(1) || g["HasRightHorn"] != nbt.Byte(1) {
		t.Errorf("goat: %v", g)
	}

	// The arrow gets its item, which the componentization then converts, and
	// its potion lands in that item's potion_contents.
	a := run(3000, entity("minecraft:arrow", "Potion", nbt.String("minecraft:poison"),
		"CustomPotionEffects", list(nbt.Compound{"Id": nbt.Byte(1), "Amplifier": nbt.Byte(0), "Duration": nbt.Int(5)})))
	item, _ := nbt.Get[nbt.Compound](a, "item")
	if item["id"] != nbt.String("minecraft:tipped_arrow") || item["count"] != nbt.Int(1) {
		t.Fatalf("arrow item: %v", a)
	}
	contents, _ := nbt.Get[nbt.Compound](item["components"].(nbt.Compound), "minecraft:potion_contents")
	effects, _ := nbt.Get[nbt.List](contents, "custom_effects")
	if contents["potion"] != nbt.String("minecraft:poison") || len(effects.Items) != 1 {
		t.Errorf("arrow potion_contents: %v", contents)
	}
	if effect, _ := nbt.CompoundView(effects.Items[0]); effect["id"] != nbt.String("minecraft:speed") {
		t.Errorf("arrow effect: %v", effect)
	}

	// A mooshroom's stew, a cloud's effects and a mob's active effects.
	m := run(3000, entity("minecraft:mooshroom", "EffectId", nbt.Byte(1), "EffectDuration", nbt.Int(100)))
	stew, _ := nbt.Get[nbt.List](m, "stew_effects")
	if entry, _ := nbt.CompoundView(stew.Items[0]); entry["id"] != nbt.String("minecraft:speed") || entry["duration"] != nbt.Int(100) || m["EffectId"] != nil {
		t.Errorf("mooshroom: %v", m)
	}
	z := run(3000, entity("minecraft:zombie", "ActiveEffects", list(nbt.Compound{"Id": nbt.Byte(5), "Amplifier": nbt.Byte(1)})))
	active, _ := nbt.Get[nbt.List](z, "active_effects")
	if e, _ := nbt.CompoundView(active.Items[0]); e["id"] != nbt.String("minecraft:strength") || e["amplifier"] != nbt.Byte(1) || z["ActiveEffects"] != nil {
		t.Errorf("active effects: %v", z)
	}

	// A spawner's data is wrapped, and the entity inside is upgraded too.
	spawner := nbt.Compound{
		"SpawnData":       entity("minecraft:zombie", "Attributes", list(nbt.Compound{"Name": nbt.String("generic.max_health"), "Base": nbt.Double(40)})),
		"SpawnPotentials": list(nbt.Compound{"Weight": nbt.Int(3), "Entity": entity("minecraft:pig")}),
	}
	fixSpawnerData(spawner, 2586)
	data, _ := nbt.Get[nbt.Compound](spawner, "SpawnData")
	inner, _ := nbt.Get[nbt.Compound](data, "entity")
	if inner["id"] != nbt.String("minecraft:zombie") || inner["attributes"] == nil {
		t.Errorf("SpawnData: %v", spawner["SpawnData"])
	}
	potentials, _ := nbt.Get[nbt.List](spawner, "SpawnPotentials")
	entry, _ := nbt.CompoundView(potentials.Items[0])
	pdata, _ := nbt.Get[nbt.Compound](entry, "data")
	pinner, _ := nbt.Get[nbt.Compound](pdata, "entity")
	if entry["weight"] != nbt.Int(3) || pinner["id"] != nbt.String("minecraft:pig") || entry["Weight"] != nil {
		t.Errorf("SpawnPotentials: %v", potentials)
	}
}
