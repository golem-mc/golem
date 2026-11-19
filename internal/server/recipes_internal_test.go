package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// A tag slot display carries a HolderSet: 0, then the tag name.
func TestPutSquareDisplayWritesATagAsAHolderSet(t *testing.T) {
	var b protocol.Buffer
	putSquareDisplay(&b, registry.BookIngredient{Tag: "minecraft:planks"})
	r := protocol.NewReader(b.Bytes())
	if typ, set, name := r.ReadVarInt(), r.ReadVarInt(), r.ReadString(32767); typ != slotDisplayTag || set != 0 || name != "minecraft:planks" {
		t.Errorf("got type %d set %d name %q", typ, set, name)
	}
	if r.Remaining() != 0 || r.Err() != nil {
		t.Errorf("leftover=%d err=%v", r.Remaining(), r.Err())
	}
}
