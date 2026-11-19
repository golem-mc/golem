package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// The ground an item or an orb slides over decides how fast it slows: both
// ticks read Block.getFriction of the block below the feet and shrink the
// sideways drift by the air drag times it. The lanes here hold the ice
// family's 0.98 against stone's 0.6, and the slime block, whose 0.8 is
// outweighed by stepOn's cut to 0.4 + |vy|*0.2 of the drift each tick.
// vanilla: ItemEntity.tick, ExperienceOrb.tick, Block.getFriction, SlimeBlock.stepOn

// TestAnItemSlidesFurtherOnIceAndShorterOnSlimeThanOnStone launches a
// stack along three lanes that differ only in the block under them. On
// stone the 0.6 friction stops it within about a block; on ice it glides
// several; on slime it stops well short of stone (about 0.73 blocks against
// 1.21). The ice lane's speed is also checked tick by tick against the
// vanilla per-tick share 0.98 × 0.98.
func TestAnItemSlidesFurtherOnIceAndShorterOnSlimeThanOnStone(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Slider")
	srv.StopTick()

	stone := servertest.BlockState(t, "minecraft:stone")
	ice := servertest.BlockState(t, "minecraft:ice")
	slime := servertest.BlockState(t, "minecraft:slime_block")
	for x := int32(0); x <= 15; x++ {
		srv.SetBlock(x, 63, 20, stone)
		srv.SetBlock(x, 63, 24, ice)
		srv.SetBlock(x, 63, 28, slime)
	}

	const startX = 1.5
	stoneID := srv.SpawnItemEntityMotion(startX, 64+entity.ItemHalfSize, 20.5, "minecraft:stone", 1, 0.5, 0, 0)
	iceID := srv.SpawnItemEntityMotion(startX, 64+entity.ItemHalfSize, 24.5, "minecraft:stone", 1, 0.5, 0, 0)
	slimeID := srv.SpawnItemEntityMotion(startX, 64+entity.ItemHalfSize, 28.5, "minecraft:stone", 1, 0.5, 0, 0)
	if stoneID == 0 || iceID == 0 || slimeID == 0 {
		t.Fatal("the sliding items did not spawn")
	}

	// Ten ticks of the ice lane, the drift shrunk each by the air drag
	// times the ice's own friction.
	decay := float64(float32(0.98)) * float64(float32(rules.BlockFriction(ice)))
	srv.TickEntities(10)
	if _, got := itemSlide(t, srv, iceID); math.Abs(got-0.5*math.Pow(decay, 10)) > 1e-9 {
		t.Errorf("the ice item's drift after ten ticks = %v, want the vanilla %v", got, 0.5*math.Pow(decay, 10))
	}

	srv.TickEntities(30)
	stoneX, _ := itemSlide(t, srv, stoneID)
	iceX, _ := itemSlide(t, srv, iceID)
	slimeX, _ := itemSlide(t, srv, slimeID)
	if iceX-startX <= stoneX-startX {
		t.Errorf("the item slid %v blocks on ice but %v on stone, want ice further", iceX-startX, stoneX-startX)
	}
	if slimeX-startX >= 0.7*(stoneX-startX) {
		t.Errorf("the item slid %v blocks on slime but %v on stone, want slime under 0.7 of stone's", slimeX-startX, stoneX-startX)
	}
	if iceX-startX < 5 {
		t.Errorf("the item slid %v blocks on ice, want it gliding several", iceX-startX)
	}
}

// itemSlide is the x and sideways drift of the item with the given entity ID
// still in the overworld.
func itemSlide(t *testing.T, srv *server.Server, id int32) (x, vx float64) {
	t.Helper()
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.ID == id {
			return it.X, it.VX
		}
	}
	t.Fatalf("item %d is gone", id)
	return 0, 0
}

// orbSlide is the x of the orb with the given entity ID still in the
// overworld.
func orbSlide(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	x, _, _, ok := srv.OrbPos(id)
	if !ok {
		t.Fatalf("orb %d is gone", id)
	}
	return x
}
