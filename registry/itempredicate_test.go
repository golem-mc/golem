package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestLockPredicatesTestComponents runs each partial component predicate
// against a matching and a nonmatching stack.
// vanilla: DataComponentPredicates, ItemPredicate.test
func TestLockPredicatesTestComponents(t *testing.T) {
	t.Parallel()
	stack := func(item, component, snbt string) protocol.ItemStack {
		id, ok := ItemID(item)
		if !ok {
			t.Fatalf("item %s", item)
		}
		value, err := nbt.ParseOrderedSNBT("{v:" + snbt + "}")
		if err != nil {
			t.Fatal(err)
		}
		s := protocol.ItemStack{ItemID: id, Count: 1}
		if !ReadComponent(&s, component, value.Entries()["v"]) {
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
		pred := DecodeItemPredicate(lock.Entries())
		if len(pred.tests) != 1 {
			t.Errorf("%s: predicate did not decode", c.name)
			continue
		}
		if !pred.Matches(c.ok) {
			t.Errorf("%s: matching stack refused", c.name)
		}
		if pred.Matches(c.bad) {
			t.Errorf("%s: nonmatching stack accepted", c.name)
		}
	}
}
