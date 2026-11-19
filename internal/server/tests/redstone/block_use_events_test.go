package redstone

// Block uses by a player post game events at the block. vanilla: CakeBlock.eat, FlowerPotBlock.useItemOn

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBlockUsesPostBlockChange: each use leaves exactly one block_change at
// the block's centre, from the player.
// vanilla: AbstractCandleBlock.extinguish, RespawnAnchorBlock.charge,
// LecternBlock.resetBookState, ChiseledBookShelfBlockEntity.updateState,
// SweetBerryBushBlock.useWithoutItem, DaylightDetectorBlock.useWithoutItem,
// SignBlock.useItemOn, LayeredCauldronBlock.lowerFillLevel, BellBlock.attemptToRing
func TestBlockUsesPostBlockChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, block, item string
		props             map[string]string
		face              int32
		cursor            [3]float32
	}{
		{"candle", "minecraft:candle", "", map[string]string{"lit": "true"}, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"respawn anchor", "minecraft:respawn_anchor", "minecraft:glowstone", nil, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"lectern", "minecraft:lectern", "minecraft:writable_book", nil, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"chiseled bookshelf", "minecraft:chiseled_bookshelf", "minecraft:book", map[string]string{"facing": "north"}, protocol.FaceNorth, [3]float32{0.75, 0.75, 0}},
		{"sweet berry bush", "minecraft:sweet_berry_bush", "", map[string]string{"age": "3"}, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"daylight detector", "minecraft:daylight_detector", "", nil, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"sign wax", "minecraft:oak_sign", "minecraft:honeycomb", nil, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"cauldron bottle", "minecraft:water_cauldron", "minecraft:glass_bottle", map[string]string{"level": "2"}, protocol.FaceTop, [3]float32{0.5, 0.5, 0.5}},
		{"bell", "minecraft:bell", "", map[string]string{"attachment": "floor", "facing": "north"}, protocol.FaceNorth, [3]float32{0.5, 0.5, 0.5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(1, 64, 4, servertest.MustState(t, tc.block, tc.props))
			if tc.item != "" {
				srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.item), Count: 1})
			}
			c.Settle()
			changes := 0
			srv.ObserveGameEvents(func(event string, x, y, z float64) {
				if event == "minecraft:block_change" && x == 1.5 && y == 64.5 && z == 4.5 {
					changes++
				}
			})
			c.PlaceBlockAtCursor(1, 64, 4, tc.face, tc.cursor[0], tc.cursor[1], tc.cursor[2])
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			if changes != 1 {
				t.Errorf("block_change events at the %s = %d, want 1", tc.name, changes)
			}
		})
	}
}
