package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestSlidingDownAHoneyBlockObtainsHoneyBlockSlide: a falling player whose box
// overlaps a honey column's side obtains slide_down_block within the 20-tick
// window the trigger is throttled to; one clear of the column does not.
// vanilla: HoneyBlock.entityInside
func TestSlidingDownAHoneyBlockObtainsHoneyBlockSlide(t *testing.T) {
	t.Parallel()
	const adv, criterion = "minecraft:adventure/honey_block_slide", "honey_block_slide"
	cases := []struct {
		name string
		x    float64
		want bool
	}{
		{"against the side", 3.28, true},
		{"clear of the column", 4.5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Slider")
			honey := servertest.MustState(t, "minecraft:honey_block", nil)
			for y := int32(64); y <= 100; y++ {
				srv.SetBlock(2, y, 0, honey)
			}
			servertest.HoldTicks(t, srv)
			y := 100.5
			c.MoveToGround(tc.x, y, 0.5, false)
			c.Settle()
			for range 25 {
				y -= 0.3
				c.MoveToGround(tc.x, y, 0.5, false)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
			}
			if got := srv.AdvancementObtained("Slider", adv, criterion); got != tc.want {
				t.Errorf("honey_block_slide obtained = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAnItemSlidesDownAHoneyBlockSide: an item pressed against a honey
// pillar's side falls at the slide's pace, while one beside a stone pillar
// reaches the floor. An item's own gravity (0.04) and drag come after the
// slide's -0.1274, so it slides about 0.165 a tick, not the living body's
// 0.1274.
// vanilla: HoneyBlock.isSlidingDown, doSlideMovement; ItemEntity.tick
func TestAnItemSlidesDownAHoneyBlockSide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Slider")
	c.MoveToGround(8.5, 64, 8.5, true) // out of pickup reach
	c.Settle()
	honey := servertest.MustState(t, "minecraft:honey_block", nil)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for y := int32(64); y <= 69; y++ {
		srv.SetBlock(0, y, 0, honey)
		srv.SetBlock(0, y, 4, stone)
	}
	servertest.HoldTicks(t, srv)
	// The slider's box reaches into the honey's cell, past the narrower
	// honey shape; the faller's touches the stone's face.
	slider := srv.SpawnItemEntity(0.5+0.4375+0.125+0.01, 69.0, 0.5, "minecraft:stone", 1)
	faller := srv.SpawnItemEntity(1+0.125+0.01, 69.0, 4.5, "minecraft:stone", 1)
	if slider == 0 || faller == 0 {
		t.Fatal("the items did not spawn")
	}
	itemY := func(id int32) float64 {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.ID == id {
				return it.Y - entity.ItemHalfSize
			}
		}
		t.Fatalf("item %d is gone", id)
		return 0
	}
	// The first five ticks are free fall: the slide takes hold only once the
	// fall is past 0.08 a tick before gravity.
	last := itemY(slider)
	for tick := range 20 {
		servertest.RunTicks(t, srv, 1)
		y := itemY(slider)
		if drop := last - y; tick >= 5 && drop > 0.17 {
			t.Errorf("tick %d: the item beside the honey fell %.4f, want at most 0.17", tick, drop)
		}
		last = y
	}
	if last <= 65.5 {
		t.Errorf("the item beside the honey is at y=%.3f after 20 ticks, want above 65.5", last)
	}
	if y := itemY(faller); y > 64.01 {
		t.Errorf("the item beside the stone is at y=%.3f after 20 ticks, want on the floor at 64", y)
	}
}
