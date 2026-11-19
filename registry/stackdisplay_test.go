package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestTooltipDisplayDropsRepeatedComponents pins that a hidden component
// named twice is kept once, on the stack and in the saved tag.
// vanilla: TooltipDisplay.CODEC (a ReferenceLinkedOpenHashSet)
func TestTooltipDisplayDropsRepeatedComponents(t *testing.T) {
	t.Parallel()
	stack := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	value := nbt.Compound{"hidden_components": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("enchantments"), nbt.String("minecraft:enchantments")}}}
	if ok, _ := readDisplayComponent(&stack, "minecraft:tooltip_display", value); !ok {
		t.Fatal("the tooltip_display did not read")
	}
	if _, hidden := stack.HiddenTooltipOf(); len(hidden) != 1 {
		t.Errorf("hidden components = %v, want one", hidden)
	}
	want := nbt.Compound{"hidden_components": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:enchantments")}}}
	if got := StackComponents(stack)["minecraft:tooltip_display"]; !nbt.Equal(got, nbt.Tag(want)) {
		t.Errorf("saved tooltip_display = %v, want %v", got, want)
	}
}

// TestBreakSoundTable pins which stored break sounds read and what each
// writes back as; the item's own default reads but stores no patch.
// vanilla: SoundEvent.CODEC, PatchedDataComponentMap.set
func TestBreakSoundTable(t *testing.T) {
	t.Parallel()
	refused := nbt.Byte(-1)
	for _, tc := range []struct {
		name  string
		item  Item
		value nbt.Tag
		want  nbt.Tag // nil: no component; refused: the value does not read
	}{
		{"registered name", ItemStick, nbt.String("item.shield.break"), nbt.String("minecraft:item.shield.break")},
		{"the default", ItemStick, nbt.String("entity.item.break"), nil},
		{"the shield's default", ItemShield, nbt.String("item.shield.break"), nil},
		{"the wolf armour's default", ItemWolfArmor, nbt.String("item.wolf_armor.break"), nil},
		{"the common default on a shield", ItemShield, nbt.String("entity.item.break"), nbt.String("minecraft:entity.item.break")},
		{"an inline default", ItemStick, nbt.Compound{"sound_id": nbt.String("entity.item.break")},
			nbt.Compound{"sound_id": nbt.String("minecraft:entity.item.break")}},
		{"unregistered name", ItemStick, nbt.String("minecraft:x"), refused},
		{"inline", ItemStick, nbt.Compound{"sound_id": nbt.String("x")}, nbt.Compound{"sound_id": nbt.String("minecraft:x")}},
		{"inline with an int range", ItemStick, nbt.Compound{"sound_id": nbt.String("minecraft:x"), "range": nbt.Int(16)},
			nbt.Compound{"sound_id": nbt.String("minecraft:x"), "range": nbt.Float(16)}},
		{"inline with a malformed range", ItemStick, nbt.Compound{"sound_id": nbt.String("minecraft:x"), "range": nbt.String("far")},
			nbt.Compound{"sound_id": nbt.String("minecraft:x")}},
		{"inline with a bad id", ItemStick, nbt.Compound{"sound_id": nbt.String("Bad Id")}, refused},
	} {
		stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
		ok, _ := readDisplayComponent(&stack, "minecraft:break_sound", tc.value)
		if ok != (tc.want != refused) {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.want != refused)
			continue
		}
		if !ok {
			continue
		}
		if got := StackComponents(stack)["minecraft:break_sound"]; !nbt.Equal(got, tc.want) {
			t.Errorf("%s: wrote back as %v, want %v", tc.name, got, tc.want)
		}
		if _, raw := stack.BreakSoundOf(); raw != (tc.want != nil) {
			t.Errorf("%s: stack carries break_sound = %v, want %v", tc.name, raw, tc.want != nil)
		}
	}
}

// TestItemModelTable pins that an item_model naming the item's own model
// stores no patch, so the stack stays the plain item.
// vanilla: Item.Properties.model, PatchedDataComponentMap.set
func TestItemModelTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  bool // the stack carries an item_model
	}{
		{"its own model", "minecraft:stick", false},
		{"its own model, unqualified", "stick", false},
		{"another item's model", "minecraft:diamond", true},
	} {
		stack := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
		if ok, _ := readDisplayComponent(&stack, "minecraft:item_model", nbt.String(tc.value)); !ok {
			t.Errorf("%s: refused", tc.name)
			continue
		}
		if _, has := stack.ItemModelOf(); has != tc.want {
			t.Errorf("%s: carries item_model = %v, want %v", tc.name, has, tc.want)
		}
		if has := !stack.Same(protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}); has != tc.want {
			t.Errorf("%s: differs from a plain stick = %v, want %v", tc.name, has, tc.want)
		}
	}
}
