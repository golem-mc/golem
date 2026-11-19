package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"slices"
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

func TestMapDecorationHistorySurvivesEditsAndWire(t *testing.T) {
	tag, err := nbt.ParseOrderedSNBT(`{BB:{type:"minecraft:red_x",x:1.0d,z:2.0d,rotation:3.0f},Aa:{type:"minecraft:red_x",x:4.0d,z:5.0d,rotation:6.0f}}`)
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.ItemStack{ItemID: int32(ItemFilledMap), Count: 1}
	if !original.SetMapDecorations(tag) {
		t.Fatal("map refused")
	}
	stack := original
	marker := protocol.MapDecoration{Type: "minecraft:red_x", X: 10, Z: 20, Rotation: 30}
	stack.SetMapDecoration("BB", marker)
	stack.SetMapDecoration("C#", marker)
	// vanilla: MapDecorations.withDecoration, Util.copyAndPut
	if keys := nbt.HashOrderedKeys(stack.MapDecorationsTag()); !slices.Equal(keys, []string{"BB", "Aa", "C#"}) {
		t.Fatalf("edited traversal: %v", keys)
	}
	if len(original.MapDecorations) != 2 || original.MapDecorations["BB"].X != 1 {
		t.Fatal("marker edit mutated the original stack")
	}
	var wire protocol.Buffer
	wire.PutSlot(stack)
	received, ok := protocol.NewReader(wire.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("map slot refused")
	}
	saved, ok := StoredStack(StackTag(received))
	if !ok {
		t.Fatal("map save refused")
	}
	if keys := nbt.HashOrderedKeys(saved.MapDecorationsTag()); !slices.Equal(keys, []string{"BB", "Aa", "C#"}) {
		t.Fatalf("wire/save traversal: %v", keys)
	}
}
