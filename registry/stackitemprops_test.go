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
