package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestPaintDragSkipsSlotsBeyondTheItems checks the paint drag's share when
// more slots are painted than there are items: a slot is marked only while
// the cursor still holds more items than the slots already marked, so two
// sticks painted over three empty slots fill the first two and leave the
// cursor empty.
func TestPaintDragSkipsSlotsBeyondTheItems(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// Two sticks onto the cursor from a hotbar slot that is not the held
	// one, so no equipment broadcast trails the pickup.
	sticks := servertest.ItemStack(t, "minecraft:stick", 2)
	from := int16(player.HotbarSlot + 1)
	w.SetCreativeSlot(from, sticks)
	w.Click(from, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{from: {}}, sticks)

	// Paint three empty main-inventory slots and let go.
	w.Click(-999, 0, protocol.ClickModeDrag)
	w.Drain()
	w.Click(9, 1, protocol.ClickModeDrag)
	w.Click(10, 1, protocol.ClickModeDrag)
	w.Click(11, 1, protocol.ClickModeDrag)
	w.Drain()
	w.Click(-999, 2, protocol.ClickModeDrag)

	one := servertest.ItemStack(t, "minecraft:stick", 1)
	w.ExpectClickResult(map[int16]protocol.ItemStack{9: one, 10: one}, protocol.ItemStack{})
}

// TestPaintDragSkipsSlotsThatHoldAnotherItem checks the paint drag's share
// when a painted slot holds something else: that slot is not marked, so ten
// sticks over an empty slot, a stone slot and another empty slot go five to
// each of the empty ones and leave the cursor empty.
func TestPaintDragSkipsSlotsThatHoldAnotherItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(10, servertest.ItemStack(t, "minecraft:stone", 5))

	sticks := servertest.ItemStack(t, "minecraft:stick", 10)
	from := int16(player.HotbarSlot + 1)
	w.SetCreativeSlot(from, sticks)
	w.Click(from, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{from: {}}, sticks)

	w.Click(-999, 0, protocol.ClickModeDrag)
	w.Drain()
	w.Click(9, 1, protocol.ClickModeDrag)
	w.Click(10, 1, protocol.ClickModeDrag)
	w.Click(11, 1, protocol.ClickModeDrag)
	w.Drain()
	w.Click(-999, 2, protocol.ClickModeDrag)

	five := servertest.ItemStack(t, "minecraft:stick", 5)
	w.ExpectClickResult(map[int16]protocol.ItemStack{9: five, 11: five}, protocol.ItemStack{})
}
