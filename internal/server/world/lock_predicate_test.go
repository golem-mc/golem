package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestLockPredicatesTestComponents runs each partial component predicate
// against a matching and a nonmatching stack.
// vanilla: DataComponentPredicates, ItemPredicate.test
func TestLockPredicatesTestComponents(t *testing.T) {
	t.Parallel()
	stack := func(item, component, snbt string) protocol.ItemStack {
		id, ok := registry.ItemID(item)
		if !ok {
			t.Fatalf("item %s", item)
		}
		value, err := nbt.ParseOrderedSNBT("{v:" + snbt + "}")
		if err != nil {
			t.Fatal(err)
		}
		s := protocol.ItemStack{ItemID: id, Count: 1}
		if !registry.ReadComponent(&s, component, value.Entries()["v"]) {
			t.Fatalf("component %s %s did not read", component, snbt)
		}
		return s
	}
	cases := []struct {
		name, predicate string
		ok, bad         protocol.ItemStack
	}{
		{"potion", `{"minecraft:potion_contents":{potions:"minecraft:swiftness"}}`,
			stack("minecraft:potion", "minecraft:potion_contents", `{potion:"minecraft:swiftness"}`),
			stack("minecraft:potion", "minecraft:potion_contents", `{potion:"minecraft:healing"}`)},
		{"potion effects", `{"minecraft:potion_contents":{effects:{contains:[{"minecraft:speed":{amplifier:{min:1}}}]}}}`,
			stack("minecraft:potion", "minecraft:potion_contents", `{custom_effects:[{id:"minecraft:speed",amplifier:1b,duration:100}]}`),
			stack("minecraft:potion", "minecraft:potion_contents", `{custom_effects:[{id:"minecraft:speed",amplifier:0b,duration:100}]}`)},
		{"firework explosion", `{"minecraft:firework_explosion":{shape:"star",has_trail:true}}`,
			stack("minecraft:firework_star", "minecraft:firework_explosion", `{shape:"star",has_trail:1b}`),
			stack("minecraft:firework_star", "minecraft:firework_explosion", `{shape:"star"}`)},
		{"fireworks", `{"minecraft:fireworks":{flight_duration:{min:2},explosions:{size:1}}}`,
			stack("minecraft:firework_rocket", "minecraft:fireworks", `{flight_duration:2b,explosions:[{shape:"star"}]}`),
			stack("minecraft:firework_rocket", "minecraft:fireworks", `{flight_duration:1b,explosions:[{shape:"star"}]}`)},
		{"trim", `{"minecraft:trim":{material:"minecraft:gold"}}`,
			stack("minecraft:iron_chestplate", "minecraft:trim", `{material:"minecraft:gold",pattern:"minecraft:coast"}`),
			stack("minecraft:iron_chestplate", "minecraft:trim", `{material:"minecraft:iron",pattern:"minecraft:coast"}`)},
		{"written book", `{"minecraft:written_book_content":{author:"Alex",generation:{min:1}}}`,
			stack("minecraft:written_book", "minecraft:written_book_content", `{title:"T",author:"Alex",generation:1}`),
			stack("minecraft:written_book", "minecraft:written_book_content", `{title:"T",author:"Alex"}`)},
		{"writable book", `{"minecraft:writable_book_content":{pages:{contains:["hi"]}}}`,
			stack("minecraft:writable_book", "minecraft:writable_book_content", `{pages:["hi"]}`),
			stack("minecraft:writable_book", "minecraft:writable_book_content", `{pages:["yo"]}`)},
		{"attributes", `{"minecraft:attribute_modifiers":{modifiers:{contains:[{attribute:"minecraft:attack_damage",amount:{min:5}}]}}}`,
			stack("minecraft:iron_sword", "minecraft:attribute_modifiers", `[{type:"minecraft:attack_damage",id:"minecraft:x",amount:6.0d,operation:"add_value"}]`),
			stack("minecraft:iron_sword", "minecraft:attribute_modifiers", `[{type:"minecraft:attack_damage",id:"minecraft:x",amount:1.0d,operation:"add_value"}]`)},
		{"bundle", `{"minecraft:bundle_contents":{items:{count:[{test:{items:"minecraft:dirt"},count:2}]}}}`,
			stack("minecraft:bundle", "minecraft:bundle_contents", `[{id:"minecraft:dirt",count:1},{id:"minecraft:dirt",count:1}]`),
			stack("minecraft:bundle", "minecraft:bundle_contents", `[{id:"minecraft:dirt",count:1}]`)},
	}
	for _, c := range cases {
		lock, err := nbt.ParseOrderedSNBT(`{predicates:` + c.predicate + `}`)
		if err != nil {
			t.Fatal(c.name, err)
		}
		pred := decodeItemPredicate(lock.Entries())
		if len(pred.tests) != 1 {
			t.Errorf("%s: predicate did not decode", c.name)
			continue
		}
		if !pred.matches(c.ok) {
			t.Errorf("%s: matching stack refused", c.name)
		}
		if pred.matches(c.bad) {
			t.Errorf("%s: nonmatching stack accepted", c.name)
		}
	}
}

// TestLockPredicatesReadDefaultsAndTags covers a rocket's default fireworks
// component, and that a #tag no pack defines is a decode failure that drops the constraint.
// vanilla: Items.FIREWORK_ROCKET, HolderSetCodec
func TestLockPredicatesReadDefaultsAndTags(t *testing.T) {
	t.Parallel()
	rocket, _ := registry.ItemID("minecraft:firework_rocket")
	plain := protocol.ItemStack{ItemID: rocket, Count: 1}
	longer := plain
	value, _ := nbt.ParseOrderedSNBT(`{v:{flight_duration:2b}}`)
	if !registry.ReadComponent(&longer, "minecraft:fireworks", value.Entries()["v"]) {
		t.Fatal("fireworks did not read")
	}
	chest, _ := registry.ItemID("minecraft:iron_chestplate")
	trimmed := protocol.ItemStack{ItemID: chest, Count: 1}
	value, _ = nbt.ParseOrderedSNBT(`{v:{material:"minecraft:gold",pattern:"minecraft:coast"}}`)
	registry.ReadComponent(&trimmed, "minecraft:trim", value.Entries()["v"])
	missing, _ := nbt.ParseOrderedSNBT(`{predicates:{"minecraft:trim":{material:"#minecraft:nothing"}}}`)
	if n := len(decodeItemPredicate(missing.Entries()).tests); n != 0 {
		t.Errorf("a missing tag left %d tests, want the constraint dropped", n)
	}
	cases := []struct {
		name, predicate string
		ok, bad         protocol.ItemStack
		decodes         bool
	}{
		{"default rocket", `{"minecraft:fireworks":{flight_duration:1,explosions:{size:0}}}`, plain, longer, true},
	}
	for _, c := range cases {
		lock, err := nbt.ParseOrderedSNBT(`{predicates:` + c.predicate + `}`)
		if err != nil {
			t.Fatal(c.name, err)
		}
		pred := decodeItemPredicate(lock.Entries())
		if len(pred.tests) != 1 {
			t.Errorf("%s: predicate did not decode", c.name)
			continue
		}
		if !pred.matches(c.ok) {
			t.Errorf("%s: matching stack refused", c.name)
		}
		if pred.matches(c.bad) {
			t.Errorf("%s: nonmatching stack accepted", c.name)
		}
	}
}

// TestLockPredicatesMatchRegistryTagSets resolves a #tag for the trim
// pattern and jukebox song sets, matching and refusing a stack
// each. Tags are process-wide: do not parallelize.
// vanilla: HolderSetCodec, TrimPredicate, JukeboxPlayablePredicate
func TestLockPredicatesMatchRegistryTagSets(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	entry := func(id string) registry.PackTag {
		return registry.PackTag{Entries: []registry.PackTagEntry{{ID: id, Required: true}}}
	}
	registry.ApplyPackTags(registry.PackTags{
		"minecraft:trim_pattern": {"pk:coasts": entry("minecraft:coast"), "pk:ribs": entry("minecraft:rib")},
		"minecraft:jukebox_song": {"pk:cats": entry("minecraft:cat"), "pk:thirteens": entry("minecraft:13")},
	})
	stack := func(item, component, snbt string) protocol.ItemStack {
		id, ok := registry.ItemID(item)
		if !ok {
			t.Fatalf("item %s", item)
		}
		value, err := nbt.ParseOrderedSNBT("{v:" + snbt + "}")
		if err != nil {
			t.Fatal(err)
		}
		s := protocol.ItemStack{ItemID: id, Count: 1}
		if !registry.ReadComponent(&s, component, value.Entries()["v"]) {
			t.Fatalf("component %s %s did not read", component, snbt)
		}
		return s
	}
	trim := stack("minecraft:iron_chestplate", "minecraft:trim", `{material:"minecraft:gold",pattern:"minecraft:coast"}`)
	cat := stack("minecraft:stick", "minecraft:jukebox_playable", `"minecraft:cat"`)
	cases := []struct {
		name, predicate string
		stack           protocol.ItemStack
		opens           bool
	}{
		{"pattern match", `{"minecraft:trim":{pattern:"#pk:coasts"}}`, trim, true},
		{"pattern refuse", `{"minecraft:trim":{pattern:"#pk:ribs"}}`, trim, false},
		{"song match", `{"minecraft:jukebox_playable":{song:"#pk:cats"}}`, cat, true},
		{"song refuse", `{"minecraft:jukebox_playable":{song:"#pk:thirteens"}}`, cat, false},
	}
	for _, c := range cases {
		lock, err := nbt.ParseOrderedSNBT(`{predicates:` + c.predicate + `}`)
		if err != nil {
			t.Fatal(c.name, err)
		}
		pred := decodeItemPredicate(lock.Entries())
		if len(pred.tests) != 1 {
			t.Errorf("%s: predicate did not decode", c.name)
			continue
		}
		if got := pred.matches(c.stack); got != c.opens {
			t.Errorf("%s: matches = %v, want %v", c.name, got, c.opens)
		}
	}
}
