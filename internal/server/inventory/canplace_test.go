package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// disabledBlock is a testBlock with some slots disabled, as the crafter's.
type disabledBlock struct {
	testBlock
	off map[int]bool
}

func (b *disabledBlock) SlotDisabled(slot int) bool { return b.off[slot] }

// TestCanPlaceItemContainerRules checks each container's own rule: the
// bottle and record slots take only while empty, the shulker box takes a
// shulker box, and a shelf slot takes up to its stack limit.
// vanilla: BrewingStandBlockEntity.canPlaceItem, JukeboxBlockEntity.canPlaceItem,
// ListBackedContainer.canPlaceItem
func TestCanPlaceItemContainerRules(t *testing.T) {
	t.Parallel()
	brewing := &testBlock{kind: KindBrewingStand, slots: make([]protocol.ItemStack, 5)}
	bottle := item(t, "minecraft:glass_bottle", 1)
	if !CanPlaceItem(brewing, bottle, 0) {
		t.Error("an empty bottle slot refused a bottle")
	}
	brewing.slots[0] = bottle
	if CanPlaceItem(brewing, bottle, 0) {
		t.Error("an occupied bottle slot took a bottle")
	}
	if CanPlaceItem(brewing, item(t, "minecraft:stone", 1), 1) {
		t.Error("a bottle slot took stone")
	}

	jukebox := &testBlock{kind: KindJukebox, slots: make([]protocol.ItemStack, 1)}
	disc := item(t, "minecraft:music_disc_13", 1)
	if !CanPlaceItem(jukebox, disc, 0) {
		t.Error("an empty jukebox refused a disc")
	}
	jukebox.slots[0] = disc
	if CanPlaceItem(jukebox, disc, 0) {
		t.Error("a loaded jukebox took a second disc")
	}

	shulker := &testBlock{kind: KindShulker, slots: make([]protocol.ItemStack, 27)}
	if !CanPlaceItem(shulker, item(t, "minecraft:shulker_box", 1), 0) {
		t.Error("a shulker box refused a shulker box; only the face rule does")
	}
	if canPlaceThroughFace(shulker, item(t, "minecraft:shulker_box", 1), 0) {
		t.Error("the face rule let a shulker box into a shulker box")
	}

	shelf := &testBlock{kind: KindShelf, slots: make([]protocol.ItemStack, 3)}
	shelf.slots[0] = item(t, "minecraft:stone", 64)
	shelf.slots[1] = item(t, "minecraft:stone", 10)
	stone := item(t, "minecraft:stone", 1)
	if CanPlaceItem(shelf, stone, 0) {
		t.Error("a full shelf slot took an item")
	}
	if !CanPlaceItem(shelf, stone, 1) || !CanPlaceItem(shelf, stone, 2) {
		t.Error("a shelf slot with room refused an item")
	}
}
