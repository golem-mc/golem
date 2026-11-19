package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestPaintDragKeepsTheCursorComponents checks the stack a paint drag leaves
// on the cursor when it does not divide the whole stack out: the remainder is
// the carried stack with its count cut down, so the components the slots were
// given - the enchantment and the name among them - ride back out in the Set
// Cursor Item packet too. The marked slots keep their components either way.
func TestPaintDragKeepsTheCursorComponents(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// A stackable item, so the creative slot's clamp to a full stack leaves
	// the three the drag divides: a sword is a stack of its own.
	carried := servertest.EnchantedStack(t, "minecraft:stick", "minecraft:sharpness", 2)
	carried.Count = 3
	name := protocol.Text{Text: "Paint"}
	carried.CustomName = &name
	w.SetCreativeSlot(36, carried)

	// Lift the whole stack onto the cursor, then paint two slots with the
	// right button: one thing lands in each and the third stays on the
	// cursor, still the enchanted, named stack it was.
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, carried)
	w.Click(-999, 4, protocol.ClickModeDrag)
	w.Drain()
	w.Click(37, 5, protocol.ClickModeDrag)
	w.Click(38, 5, protocol.ClickModeDrag)
	w.Drain()
	w.Click(-999, 6, protocol.ClickModeDrag)

	one := carried
	one.Count = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		37: one,
		38: one,
	}, one)
}
