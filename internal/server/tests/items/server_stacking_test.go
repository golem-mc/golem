package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// blockProps names the block at a position and reads its properties.
func blockProps(t *testing.T, srv *server.Server, x, y, z int32) (string, map[string]string) {
	t.Helper()
	name, raw, ok := registry.BlockStateName(srv.BlockAt(x, y, z))
	if !ok {
		t.Fatalf("block at (%d,%d,%d) has no registry name", x, y, z)
	}
	return name, registry.ParseStateProps(raw)
}

// stackingClient joins a player standing beside (5, 64, 5), the cell the
// stacking tests build in.
func stackingClient(t *testing.T, survival bool, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	if survival {
		cfg = servertest.SurvivalTestConfig()
	}
	srv, c := servertest.Join(t, cfg, name)
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	return srv, c
}

// TestLeafLitterStacksToFourSegments: litter clicked with
// litter adds a segment (SegmentableBlock.canBeReplaced), each costing one
// item in survival, up to four; a fifth leaves the full cell alone.
func TestLeafLitterStacksToFourSegments(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, true, "Raker")
	litter, ok := registry.ItemID("minecraft:leaf_litter")
	if !ok {
		t.Fatal("the item registry has no leaf litter")
	}
	srv.GiveSlot("Raker", player.HotbarSlot, protocol.ItemStack{ItemID: litter, Count: 5})

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	for want := 1; want <= 4; want++ {
		if want > 1 {
			c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 1.0/16)
			c.ExpectBlockAck()
		}
		name, props := blockProps(t, srv, 5, 64, 5)
		if name != "minecraft:leaf_litter" || props["segment_amount"] != strconv.Itoa(want) {
			t.Fatalf("after %d placements the cell is %s %v, want leaf_litter segment_amount=%d", want, name, props, want)
		}
		if got := srv.InventorySlot("Raker", player.HotbarSlot); got.Count != int32(5-want) {
			t.Errorf("after %d placements the hand holds %d, want %d", want, got.Count, 5-want)
		}
	}

	c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 1.0/16)
	c.ExpectBlockAck()
	if _, props := blockProps(t, srv, 5, 64, 5); props["segment_amount"] != "4" {
		t.Errorf("a fifth litter made segment_amount=%s, want 4", props["segment_amount"])
	}
}

// TestTwoSlabsMakeADoubleSlab checks SlabBlock.canBeReplaced: a slab clicked
// on the top of a bottom slab of its kind fills it to a double slab, one
// clicked on the side goes beside it, and another kind does not merge.
func TestTwoSlabsMakeADoubleSlab(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Mason")
	servertest.HoldItem(t, c, "minecraft:oak_slab")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Fatalf("first slab is %s %v, want a bottom oak slab", name, props)
	}

	// The side of a bottom slab: the slab goes in the cell beside it.
	c.PlaceBlockAt(5, 64, 5, protocol.FaceNorth, 0.25)
	c.ExpectBlockAck()
	if _, props := blockProps(t, srv, 5, 64, 5); props["type"] != "bottom" {
		t.Errorf("a side click turned the clicked slab %s, want it left bottom", props["type"])
	}
	if name, props := blockProps(t, srv, 5, 64, 4); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Errorf("the side click placed %s %v beside, want a bottom oak slab", name, props)
	}

	// Another kind on the top half stays out of the oak slab.
	servertest.HoldItem(t, c, "minecraft:stone_slab")
	c.PlaceBlockAt(5, 64, 4, protocol.FaceTop, 0.5)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 4); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Errorf("a stone slab on an oak slab made it %s %v, want it left a bottom oak slab", name, props)
	}

	// The top of the bottom slab: the two make a double slab.
	servertest.HoldItem(t, c, "minecraft:oak_slab")
	c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 0.5)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:oak_slab" ||
		props["type"] != "double" || props["waterlogged"] != "false" {
		t.Errorf("slab on slab is %s %v, want a dry double oak slab", name, props)
	}
	if got := srv.BlockAt(5, 65, 5); got != 0 {
		t.Errorf("the cell above the double slab holds %d, want air", got)
	}
}

// TestSnowLayersStackByClicking checks SnowLayerBlock: snow clicked on the
// top of a snow layer adds a layer in the same cell.
func TestSnowLayersStackByClicking(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Plougher")
	servertest.HoldItem(t, c, "minecraft:snow")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:snow" || props["layers"] != "1" {
		t.Fatalf("first snow is %s %v, want snow layers=1", name, props)
	}
	c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 2.0/16)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:snow" || props["layers"] != "2" {
		t.Errorf("snow on snow is %s %v, want snow layers=2", name, props)
	}
	if got := srv.BlockAt(5, 65, 5); got != 0 {
		t.Errorf("the cell above the snow holds %d, want air", got)
	}
}

// TestCandlesOfOneColourStack checks CandleBlock.canBeReplaced: candles of
// one colour gather into one cell, another colour does not join them.
func TestCandlesOfOneColourStack(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Chandler")
	servertest.HoldItem(t, c, "minecraft:white_candle")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	for range 2 {
		c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 6.0/16)
		c.ExpectBlockAck()
	}
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:white_candle" || props["candles"] != "3" {
		t.Fatalf("three white candles make %s %v, want white_candle candles=3", name, props)
	}

	servertest.HoldItem(t, c, "minecraft:red_candle")
	c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 6.0/16)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:white_candle" || props["candles"] != "3" {
		t.Errorf("a red candle on three white ones made %s %v, want white_candle candles=3", name, props)
	}
}

// TestSeaPicklesStackToFour checks SeaPickleBlock: pickles gather one per
// click up to four, and a pickle placed out of water is not waterlogged.
func TestSeaPicklesStackToFour(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Pickler")
	servertest.HoldItem(t, c, "minecraft:sea_pickle")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	if _, props := blockProps(t, srv, 5, 64, 5); props["waterlogged"] != "false" {
		t.Errorf("a pickle placed on dry ground is waterlogged=%s, want false", props["waterlogged"])
	}
	for range 3 {
		c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 6.0/16)
		c.ExpectBlockAck()
	}
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:sea_pickle" || props["pickles"] != "4" {
		t.Errorf("four pickles make %s %v, want sea_pickle pickles=4", name, props)
	}
}

// TestAVineClickedWithAVineAddsNoEmptyVine checks VineBlock: a vine takes
// the wall the click came through, and a vine clicked with a vine and no
// other wall to take stays as it was, with nothing placed beside it.
func TestAVineClickedWithAVineAddsNoEmptyVine(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Gardener")
	if !srv.SetBlock(6, 64, 5, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the world did not take the wall")
	}
	servertest.HoldItem(t, c, "minecraft:vine")

	c.PlaceBlock(6, 64, 5, protocol.FaceWest)
	c.ExpectBlockAck()
	name, props := blockProps(t, srv, 5, 64, 5)
	if name != "minecraft:vine" || props["east"] != "true" {
		t.Fatalf("vine against the wall is %s %v, want a vine with east=true", name, props)
	}

	c.PlaceBlock(5, 64, 5, protocol.FaceWest)
	c.ExpectBlockAck()
	if _, again := blockProps(t, srv, 5, 64, 5); again["east"] != "true" || vineFaceCount(again) != 1 {
		t.Errorf("a vine on the vine made it %v, want it unchanged", again)
	}
	if got := srv.BlockAt(4, 64, 5); got != 0 {
		t.Errorf("a vine on the vine put %d beside it, want air", got)
	}
}

func vineFaceCount(props map[string]string) int {
	n := 0
	for _, f := range []string{"up", "north", "south", "west", "east"} {
		if props[f] == "true" {
			n++
		}
	}
	return n
}

// TestAVineHangsOnLeaves checks MultifaceBlock.canAttachTo's collision
// half: leaves have no support shape, but their full collision cube holds a
// vine, the way every jungle tree carries them.
func TestAVineHangsOnLeaves(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Climber")
	leaves, ok := registry.FindState("minecraft:jungle_leaves", map[string]string{"persistent": "true"})
	if !ok || !srv.SetBlock(6, 64, 5, leaves) {
		t.Fatal("the world did not take the leaves")
	}
	servertest.HoldItem(t, c, "minecraft:vine")

	c.PlaceBlock(6, 64, 5, protocol.FaceWest)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:vine" || props["east"] != "true" {
		t.Errorf("vine against the leaves is %s %v, want a vine with east=true", name, props)
	}
}

// TestPaleMossCarpetTakesSidesNextToAWall places a carpet beside stone: the
// side facing the stone is low (MossyCarpetBlock.getStateForPlacement), and
// it goes back to none once the stone is broken (updateShape).
func TestPaleMossCarpetTakesSidesNextToAWall(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Mosser")
	stone, _ := registry.FindState("minecraft:stone", nil)
	if !srv.SetBlock(6, 64, 5, stone) {
		t.Fatal("the world did not take the stone")
	}
	servertest.HoldItem(t, c, "minecraft:pale_moss_carpet")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	name, props := blockProps(t, srv, 5, 64, 5)
	if name != "minecraft:pale_moss_carpet" || props["east"] != "low" || props["west"] != "none" || props["bottom"] != "true" {
		t.Fatalf("carpet beside stone is %s %v, want east=low west=none", name, props)
	}

	c.BreakBlock(6, 64, 5)
	c.Settle()
	servertest.WaitForTicks(t, srv, 20, func() bool {
		_, raw, _ := registry.BlockStateName(srv.BlockAt(5, 64, 5))
		return registry.ParseStateProps(raw)["east"] == "none"
	}, "the carpet's east side did not go back to none")
}
