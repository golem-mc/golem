package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// A candle stands on a full top face and fire on a full top face too, so
// neither goes on a cake's half-height top: the candle clicks are refused,
// while the candle-cake interaction and the lighting of an unlit candle cake
// still answer. vanilla: CandleBlock.canSurvive, FireBlock.canSurvive

// TestACandleStaysOffABittenCake clicks the top of a cut cake and of a candle
// cake with a candle in hand: neither a full top, so the placement is refused
// with the refusal echoes, the cake left as it stood and the candle unspent.
func TestACandleStaysOffABittenCake(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stick")

	for _, tc := range []struct {
		name  string
		state uint16
	}{
		{"a cut cake", servertest.BlockStateWith(t, "minecraft:cake", "bites=2")},
		{"a candle cake", servertest.BlockStateWith(t, "minecraft:red_candle_cake", "lit=false")},
	} {
		srv.SetBlock(3, 64, 0, tc.state)
		// Full food: a cut cake's click is a bite otherwise, and a full
		// eater is refused it, which is what leaves the item's placement.
		srv.SetFood("Actor", 20)
		srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:red_candle"), Count: 1})

		actor.PlaceBlock(3, 64, 0, protocol.FaceTop)
		actor.SettleKeeping()
		srv.FlushBlockAcks()
		if seq := actor.ExpectBlockAck(); seq != 1 {
			t.Errorf("the %s candle click's ack = %d, want 1", tc.name, seq)
		}
		if echoes := actor.ExpectUseEchoes(3, 64, 0, protocol.FaceTop); echoes[0] != tc.state || echoes[1] != 0 {
			t.Errorf("the %s refusal echoes = %v, want the unchanged cake (%d) and air", tc.name, echoes, tc.state)
		}
		actor.Settle()

		if got := srv.BlockAt(3, 64, 0); got != tc.state {
			t.Errorf("%s after a candle = %d, want %d (unchanged)", tc.name, got, tc.state)
		}
		if got := srv.BlockAt(3, 65, 0); got != 0 {
			t.Errorf("above %s = %d, want air (no candle went up)", tc.name, got)
		}
		if got := srv.InventorySlot("Actor", player.HotbarSlot).Count; got != 1 {
			t.Errorf("candles left after clicking %s = %d, want 1 (unspent)", tc.name, got)
		}
	}
}

// TestACandleCakePopsWhenItsFloorGoes removes the block under a candle cake:
// it breaks and drops its candle, like a plain cake.
// vanilla: CandleCakeBlock.updateShape
func TestACandleCakePopsWhenItsFloorGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for i, tc := range []struct{ cake, drop string }{
		{"candle_cake", "minecraft:candle"},
		{"red_candle_cake", "minecraft:red_candle"},
	} {
		x := int32(8 + 2*i)
		servertest.Setblock(t, c, x, 64, 6, "stone")
		servertest.Setblock(t, c, x, 65, 6, tc.cake)
		if got, want := servertest.RemoveAndAwaitDrop(t, c, x, 64, 6), servertest.ItemIDOrDie(t, tc.drop); got != want {
			t.Errorf("%s dropped item %d, want %s (%d)", tc.cake, got, tc.drop, want)
		}
		if got := srv.BlockAt(x, 65, 6); got != 0 {
			t.Errorf("%s stands as state %d after its floor went, want air", tc.cake, got)
		}
	}
}
