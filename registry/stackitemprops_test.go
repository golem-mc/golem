package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func snbt(t *testing.T, text string) nbt.Tag {
	t.Helper()
	tag, _, err := nbt.ParseOrderedValueTag(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return tag
}

// TestItemPropComponentsRoundTrip reads each component from SNBT, saves it,
// reloads it and sends it over the wire, and checks a refused value.
// vanilla: DataComponents.MAX_DAMAGE and the other prototype overrides
func TestItemPropComponentsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item, component, valid, refused string
	}{
		{"diamond_sword", "max_damage", "10", "0"},
		{"stick", "enchantable", "{value:5}", "{value:0}"},
		{"stick", "repairable", `{items:"minecraft:diamond"}`, `{items:"minecraft:nothing"}`},
		{"stick", "repairable", `{items:["minecraft:diamond","minecraft:stick"]}`, `{items:"#minecraft:no_such_tag"}`},
		{"stick", "repairable", `{items:"#minecraft:planks"}`, `{}`},
		{"stick", "dye", `"red"`, `"mauve"`},
		{"stick", "damage_type", `"minecraft:arrow"`, `"minecraft:nope"`},
		{"stick", "provides_trim_material", `"minecraft:gold"`, `"minecraft:nope"`},
		{"stick", "provides_banner_patterns", `"#minecraft:pattern_item/flower"`, `"#minecraft:nope"`},
		{"stick", "provides_pottery_pattern", `"minecraft:angler"`, `"minecraft:nope"`},
		{"stick", "minimum_attack_charge", "0.5f", "1.5f"},
	} {
		name := "minecraft:" + tc.component
		build := func(value string) (protocol.ItemStack, bool) {
			return StoredStack(nbt.Compound{
				"id": nbt.String("minecraft:" + tc.item), "count": nbt.Int(1),
				"components": nbt.Compound{name: snbt(t, value)},
			})
		}
		if _, ok := build(tc.refused); ok {
			t.Errorf("%s=%s was accepted", tc.component, tc.refused)
		}
		stack, ok := build(tc.valid)
		if !ok {
			t.Errorf("%s=%s was refused", tc.component, tc.valid)
			continue
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("%s: save round trip = %+v, %v", tc.component, back, ok)
		}
		var b protocol.Buffer
		b.PutSlot(stack)
		wire, ok := protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok || !stack.Equal(wire) {
			t.Errorf("%s: wire round trip = %+v, %v", tc.component, wire, ok)
		}
		saved, _ := nbt.CompoundView(StackTag(stack).Entries()["components"])
		if _, ok := saved[name]; !ok {
			t.Errorf("%s: not written to the saved stack", tc.component)
		}
	}
}

func TestEffectiveItemProps(t *testing.T) {
	t.Parallel()
	stick := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	sword := protocol.ItemStack{ItemID: int32(ItemDiamondSword), Count: 1}
	if StackMaxDamage(stick) != 0 || StackMaxDamage(sword) == 0 {
		t.Fatal("prototype max damage")
	}
	if !StackComponentsValid(stick) || !StackComponentsValid(sword) {
		t.Fatal("plain stacks are valid")
	}
	own, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:max_damage": nbt.Int(10), "minecraft:max_stack_size": nbt.Int(1)}})
	if !ok || StackMaxDamage(own) != 10 {
		t.Fatalf("stick with max_damage 10 = %+v, %v", own, ok)
	}
	if _, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:max_damage": nbt.Int(10)}}); ok {
		t.Fatal("a damageable stackable stick was accepted")
	}
	noDamage, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:diamond_sword"), "count": nbt.Int(1),
		"components": nbt.Compound{"!minecraft:max_damage": nbt.Compound{}, "minecraft:max_stack_size": nbt.Int(8)}})
	if !ok || StackMaxDamage(noDamage) != 0 {
		t.Fatalf("a sword without max_damage = %+v, %v", noDamage, ok)
	}
	ench, _ := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:enchantable": snbt(t, "{value:7}"), "minecraft:repairable": snbt(t, `{items:"minecraft:diamond"}`)}})
	diamond, _ := ItemID("minecraft:diamond")
	if StackEnchantable(ench) != 7 || !StackRepairs(ench, diamond) || StackRepairs(ench, int32(ItemStick)) {
		t.Fatal("own enchantable and repairable")
	}
}
