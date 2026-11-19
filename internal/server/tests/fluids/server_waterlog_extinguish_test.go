package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

var waterlogFires = []struct{ block, sound string }{
	{"minecraft:campfire", "minecraft:entity.generic.extinguish_fire"},
	{"minecraft:soul_campfire", "minecraft:entity.generic.extinguish_fire"},
	{"minecraft:candle", "minecraft:block.candle.extinguish"},
	{"minecraft:red_candle", "minecraft:block.candle.extinguish"},
}

func expectWaterDouse(t *testing.T, c *servertest.Client, sound string) {
	t.Helper()
	heard := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
		if name != sound {
			return
		}
		heard++
		if category != protocol.SoundCategoryBlock || volume != 1 || pitch != 1 {
			t.Errorf("extinguish = category %d, volume %g, pitch %g; want blocks, 1, 1", category, volume, pitch)
		}
	})
	if heard != 1 {
		t.Errorf("heard %s %d times, want once", sound, heard)
	}
}

func TestWaterBucketExtinguishesWaterloggedFires(t *testing.T) {
	t.Parallel()
	for _, tc := range waterlogFires {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			name, props, held := pourBesideTheFace(t, tc.block,
				map[string]string{"lit": "true", "waterlogged": "false"},
				func(c *servertest.Client) { expectWaterDouse(t, c, tc.sound) })
			if name != tc.block || props["waterlogged"] != "true" || props["lit"] != "false" {
				t.Errorf("after pour = %s %v, want waterlogged and unlit %s", name, props, tc.block)
			}
			if held != servertest.ItemIDOrDie(t, "minecraft:bucket") {
				t.Errorf("held = %d, want empty bucket", held)
			}
		})
	}
}

func TestWaterFlowExtinguishesWaterloggedFires(t *testing.T) {
	t.Parallel()
	for _, tc := range waterlogFires {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			srv, c := waterloggedScene(t)
			// Two neighbouring sources let the incoming flow become a source,
			// which LiquidBlockContainer accepts.
			servertest.SetFarmBlock(t, c, srv, 10, 64, 0, tc.block, map[string]string{"lit": "true", "waterlogged": "false"})
			servertest.SetFarmBlock(t, c, srv, 9, 64, 0, "minecraft:water", nil)
			servertest.SetFarmBlock(t, c, srv, 11, 64, 0, "minecraft:water", nil)
			servertest.HoldTicks(t, srv)
			servertest.RunTicks(t, srv, 10)
			name, props := servertest.StateProps(srv.BlockAt(10, 64, 0))
			if name != tc.block || props["waterlogged"] != "true" || props["lit"] != "false" {
				t.Fatalf("after flow = %s %v, want waterlogged and unlit %s", name, props, tc.block)
			}
			expectWaterDouse(t, c, tc.sound)
		})
	}
}
