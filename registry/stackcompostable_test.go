package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestCompostableStoredStack(t *testing.T) {
	t.Parallel()
	for _, patch := range []nbt.Compound{
		{"minecraft:compostable": nbt.Compound{"layers": nbt.Int(7)}},
		{"minecraft:compostable": nbt.Compound{"layers": nbt.String("pk:layers")}},
		{"!minecraft:compostable": nbt.Compound{}},
	} {
		stack, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:apple"), "count": nbt.Int(2), "components": patch})
		if !ok {
			t.Fatalf("refused patch %v", patch)
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("save round trip = %+v, %v, want %+v", back, ok, stack)
		}
		var b protocol.Buffer
		b.PutSlot(back)
		back, ok = protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok || !stack.Equal(back) {
			t.Errorf("wire after save = %+v, %v", back, ok)
		}
	}
	for _, layers := range []nbt.Tag{nbt.Compound{"type": nbt.String("minecraft:uniform")}, nbt.String("BAD")} {
		stack := protocol.ItemStack{ItemID: int32(ItemApple), Count: 1}
		if ReadComponent(&stack, "minecraft:compostable", nbt.Compound{"layers": layers}) {
			t.Errorf("accepted invalid layers %v", layers)
		}
	}
}
