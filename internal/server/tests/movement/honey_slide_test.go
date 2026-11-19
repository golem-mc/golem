package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

// TestAZombieSlidesDownAHoneyBlockSide: a zombie against a honey pillar's
// side plays the slide sound on the way down, and the slide's reset fall
// leaves it unhurt on landing.
// vanilla: HoneyBlock.entityInside, maybeDoSlideEffects
func TestAZombieSlidesDownAHoneyBlockSide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Slider")
	honey := servertest.MustState(t, "minecraft:honey_block", nil)
	for y := int32(64); y <= 69; y++ {
		srv.SetBlock(0, y, 0, honey)
	}
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000) // midnight: no sunlight burn to blur the landing's
	zombie := srv.SpawnMob("minecraft:zombie", 0.5+0.4375+0.3+0.01, 70, 0.5)
	if zombie == 0 {
		t.Fatal("the zombie did not spawn")
	}
	slide, _ := registry.SoundEventID("minecraft:block.honey_block.slide")
	heard := c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySoundEffect && servertest.DecodePlaySoundEffect(t, data).RegistryID == slide
	})
	if !heard {
		t.Fatal("no honey slide sound within 40 ticks")
	}
	servertest.UntilTicks(t, srv, 60, "the zombie landing", func() bool {
		_, y, _, _ := srv.MobPos(zombie)
		return y < 64.01
	})
	servertest.RunTicks(t, srv, 2)
	if h, ok := srv.MobHealth(zombie); !ok || h < 20 {
		t.Errorf("the zombie's health is %v (alive %v) after the slide, want 20", h, ok)
	}
}

// TestAZombieLandingOnHoneyPlaysTheSlide: a zombie dropped onto a honey
// block's top plays the slide sound and sends entity event 54.
// vanilla: HoneyBlock.fallOn
func TestAZombieLandingOnHoneyPlaysTheSlide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.SetBlock(0, 64, 0, servertest.MustState(t, "minecraft:honey_block", nil))
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000) // midnight: no sunlight burn
	zombie := srv.SpawnMob("minecraft:zombie", 0.5, 67, 0.5)
	if zombie == 0 {
		t.Fatal("the zombie did not spawn")
	}
	slide, _ := registry.SoundEventID("minecraft:block.honey_block.slide")
	var heard, event bool
	c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			heard = heard || servertest.DecodePlaySoundEffect(t, data).RegistryID == slide
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			event = event || r.ReadI32() == zombie && r.ReadU8() == protocol.EntityEventHoneySlide
		}
		return heard && event
	})
	if !heard || !event {
		t.Errorf("landing on honey: slide sound %v, entity event 54 %v; want both", heard, event)
	}
}
