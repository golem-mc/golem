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
