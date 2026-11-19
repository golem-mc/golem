package redstone

// Block uses by a player post game events at the block. vanilla: CakeBlock.eat, FlowerPotBlock.useItemOn

import (
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestBitingACakePostsEat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		bites string
		want  []string
	}{
		{"0", []string{"minecraft:eat"}},
		{"6", []string{"minecraft:eat", "minecraft:block_destroy"}},
	} {
		t.Run("bites "+tc.bites, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetFood("Actor", 10)
			srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:cake", map[string]string{"bites": tc.bites}))
			c.Settle()
			var got []string
			srv.ObserveGameEvents(func(event string, x, y, z float64) {
				if x == 1.5 && y == 64.5 && z == 4.5 {
					got = append(got, event)
				}
			})
			c.UseBlock(1, 64, 4)
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			if !slices.Equal(got, tc.want) {
				t.Errorf("events at the cake = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPottingAFlowerPostsBlockChange(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:flower_pot", nil))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:dandelion"), Count: 1})
	c.Settle()
	var got []string
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if x == 1.5 && y == 64.5 && z == 4.5 {
			got = append(got, event)
		}
	})
	c.UseBlock(1, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if len(got) != 1 || got[0] != "minecraft:block_change" {
		t.Errorf("events at the pot = %v, want [block_change]", got)
	}
}

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

// observeAt counts the events of one name posted at a block's centre.
func observeAt(srv *server.Server, event string, x, y, z int32) *int {
	n := new(int)
	srv.ObserveGameEvents(func(e string, ex, ey, ez float64) {
		if e == event && ex == float64(x)+0.5 && ey == float64(y)+0.5 && ez == float64(z)+0.5 {
			*n++
		}
	})
	return n
}

// vanilla: DecoratedPotBlock.useItemOn, useWithoutItem
func TestADecoratedPotUsePostsBlockChange(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:decorated_pot", nil))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:stick"), Count: 2})
	c.Settle()
	n := observeAt(srv, "minecraft:block_change", 1, 64, 4)
	c.UseBlock(1, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if *n != 1 {
		t.Errorf("block_change after an accepted stick = %d, want 1", *n)
	}
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	c.UseBlock(1, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if *n != 2 {
		t.Errorf("block_change after a bare-hand click = %d, want 2", *n)
	}
}

// vanilla: CampfireBlockEntity.placeFood, cookTick
func TestACampfirePostsBlockChangeForPlacedAndCookedFood(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "true"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:beef"), Count: 1})
	c.Settle()
	n := observeAt(srv, "minecraft:block_change", 1, 64, 4)
	c.UseBlock(1, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if *n != 1 {
		t.Fatalf("block_change after placing = %d, want 1", *n)
	}
	servertest.RunTicks(t, srv, 620)
	if *n != 2 {
		t.Errorf("block_change after cooking = %d, want 2", *n)
	}
}

// vanilla: ComposterBlock.addLayer
func TestAComposterPostsBlockChangeOnlyWhenTheLevelRises(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:composter", map[string]string{"level": "1"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat_seeds"), Count: 8})
	c.Settle()
	n := observeAt(srv, "minecraft:block_change", 1, 64, 4)
	for range 8 {
		c.UseBlock(1, 64, 4)
		c.Settle()
	}
	servertest.RunTicks(t, srv, 2)
	_, props := servertest.StatePropsOf(t, srv, 1, 64, 4)
	level, _ := strconv.Atoi(props["level"])
	if *n != level-1 {
		t.Errorf("block_change = %d, final level %d, want %d", *n, level, level-1)
	}
}
