package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestMaxStackSizeStoredStack(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item  string
		patch nbt.Compound
		want  int32 // 0: refused
	}{
		{"minecraft:stick", nbt.Compound{"minecraft:max_stack_size": nbt.Int(16)}, 16},
		{"minecraft:stick", nbt.Compound{"minecraft:max_stack_size": nbt.Int(99)}, 99},
		{"minecraft:stick", nbt.Compound{"minecraft:max_stack_size": nbt.Int(100)}, 0},
		{"minecraft:stick", nbt.Compound{"minecraft:max_stack_size": nbt.Int(0)}, 0},
		{"minecraft:stick", nbt.Compound{"!minecraft:max_stack_size": nbt.Compound{}}, 1},
		{"minecraft:diamond_sword", nbt.Compound{"minecraft:max_stack_size": nbt.Int(64)}, 0},
		{"minecraft:diamond_sword", nbt.Compound{"minecraft:max_stack_size": nbt.Int(1)}, 1},
	} {
		stack, ok := StoredStack(nbt.Compound{"id": nbt.String(tc.item), "count": nbt.Int(1), "components": tc.patch})
		if ok != (tc.want != 0) {
			t.Fatalf("%s %v: ok = %v", tc.item, tc.patch, ok)
		}
		if !ok {
			continue
		}
		if got := StackMaxStackSize(stack); got != tc.want {
			t.Errorf("%s %v: size = %d, want %d", tc.item, tc.patch, got, tc.want)
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("save round trip = %+v, %v, want %+v", back, ok, stack)
		}
		var b protocol.Buffer
		b.PutSlot(back)
		back, ok = protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok || !stack.Equal(back) || StackMaxStackSize(back) != tc.want {
			t.Errorf("wire after save = %+v, %v", back, ok)
		}
	}
}

func TestComponentAtItemDefaultLeavesNoPatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item, component string
		own, other            nbt.Tag
	}{
		{"max_stack_size", "minecraft:stone", "minecraft:max_stack_size", nbt.Int(64), nbt.Int(16)},
		{"max_damage", "minecraft:diamond_sword", "minecraft:max_damage", nbt.Int(1561), nbt.Int(10)},
		{"enchantable", "minecraft:diamond_sword", "minecraft:enchantable", nbt.Compound{"value": nbt.Int(10)}, nbt.Compound{"value": nbt.Int(3)}},
		{"use_remainder", "minecraft:honey_bottle", "minecraft:use_remainder",
			nbt.Compound{"id": nbt.String("minecraft:glass_bottle")}, nbt.Compound{"id": nbt.String("minecraft:stick")}},
	} {
		id, ok := ComponentID(tc.component)
		if !ok {
			t.Fatalf("%s: no component ID", tc.name)
		}
		read := func(value nbt.Tag) (protocol.ItemStack, bool) {
			return StoredStack(nbt.Compound{"id": nbt.String(tc.item), "count": nbt.Int(1),
				"components": nbt.Compound{tc.component: value}})
		}
		plain, _ := StoredStack(nbt.Compound{"id": nbt.String(tc.item), "count": nbt.Int(1)})
		stack, ok := read(tc.own)
		if !ok {
			t.Fatalf("%s: default value refused", tc.name)
		}
		if !stack.Equal(plain) || len(stack.Raw) != 0 || stack.Removes(id) {
			t.Errorf("%s at default = %+v, want the plain stack", tc.name, stack)
		}
		if _, has := StackTag(stack).Entries()["components"]; has {
			t.Errorf("%s at default saves components", tc.name)
		}
		stack, ok = read(tc.other)
		if !ok || stack.Equal(plain) || len(stack.Raw) == 0 {
			t.Errorf("%s at a non-default value = %+v, %v, want a patched stack", tc.name, stack, ok)
		}
	}
}
