package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func namedTable() protocol.ItemStack {
	name := protocol.Text{Text: "Altar"}
	return protocol.ItemStack{ItemID: int32(registry.ItemEnchantingTable), Count: 1, CustomName: &name}
}

// A placed table keeps its item's name and titles its window with it.
// vanilla: EnchantingTableBlockEntity.applyImplicitComponents, EnchantingTableBlock.getMenuProvider
func TestANamedEnchantingTableTitlesItsWindow(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(36, namedTable())
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	_, title := w.ExpectOpenTitle(protocol.MenuEnchantment, -1, 4, 64, 0)
	if title.Plain() != "Altar" {
		t.Errorf("window title = %q, want Altar", title.Plain())
	}
}
