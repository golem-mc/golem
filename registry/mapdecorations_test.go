package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"testing"
)

func TestExplorerMapComponentsSurviveSavingAndTheWire(t *testing.T) {
	stack := protocol.ItemStack{ItemID: int32(ItemFilledMap), Count: 1,
		MapDecorations: map[string]protocol.MapDecoration{"+": {Type: "minecraft:monument", X: -120, Z: 450, Rotation: 180}}}
	stack.SetMapID(42)

	saved, ok := StoredStack(StackTag(stack))
	if !ok || !saved.Equal(stack) {
		t.Fatalf("saved map = %+v, ok %v", saved, ok)
	}
	orderedTag, _ := nbt.CompoundView(nbt.OrderedCopy(StackTag(stack)))
	ordered, ok := StoredStack(orderedTag)
	if !ok || !ordered.Equal(stack) {
		t.Fatalf("ordered saved map = %+v, ok %v", ordered, ok)
	}
	var wire protocol.Buffer
	wire.PutSlot(saved)
	// The fallback network codec sends the target decorations as NBT.
	reader := protocol.NewReader(wire.Bytes())
	received, readOK := reader.ReadSlot()
	if !readOK || reader.Err() != nil || reader.Remaining() != 0 {
		t.Fatal("invalid map slot", reader.Err())
	}
	if !received.Equal(saved) {
		t.Fatal("wire map lost its components")
	}
	if id, ok := received.MapIDOf(); !ok || id != 42 {
		t.Fatalf("wire ID %d, %v", id, ok)
	}
	other := stack
	other.MapDecorations = map[string]protocol.MapDecoration{"+": {Type: "minecraft:mansion", X: -120, Z: 450, Rotation: 180}}
	if stack.Same(other) {
		t.Fatal("different target decorations merged")
	}
}
