package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

func TestDispenserWaterExtinguishesWaterloggedFires(t *testing.T) {
	t.Parallel()
	for _, tc := range waterlogFires {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			srv, c := waterloggedScene(t)
			if !srv.PlaceContainer(10, 64, 0, "minecraft:dispenser") {
				t.Fatal("no dispenser")
			}
			servertest.SetFarmBlock(t, c, srv, 10, 64, 1, tc.block, map[string]string{"lit": "true", "waterlogged": "false"})
			if !srv.SetInventoryAt(10, 64, 0, 0, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1}) {
				t.Fatal("no bucket")
			}
			srv.Dispense(10, 64, 0, protocol.FaceSouth)
			name, props := servertest.StateProps(srv.BlockAt(10, 64, 1))
			if name != tc.block || props["waterlogged"] != "true" || props["lit"] != "false" {
				t.Fatalf("after dispense = %s %v, want waterlogged and unlit %s", name, props, tc.block)
			}
			expectWaterDouse(t, c, tc.sound)
			if got := srv.InventoryAt(10, 64, 0)[0]; registry.Item(got.ItemID) != registry.ItemBucket {
				t.Errorf("dispenser slot = %+v, want bucket", got)
			}
		})
	}
}

// TestWaterDousingPostsBlockChange distinguishes extinguishing a fire from
// merely adding water to an unlit block.
func TestWaterDousingPostsBlockChange(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:campfire", "minecraft:candle"} {
		for _, mode := range []string{"bucket", "dispenser", "flow"} {
			for _, lit := range []string{"true", "false"} {
				t.Run(block+"/"+mode+"/lit="+lit, func(t *testing.T) {
					t.Parallel()
					srv, c := waterloggedScene(t)
					servertest.SetFarmBlock(t, c, srv, 10, 64, 1, block, map[string]string{"lit": lit, "waterlogged": "false"})
					if mode == "dispenser" {
						if !srv.PlaceContainer(10, 64, 0, "minecraft:dispenser") {
							t.Fatal("no dispenser")
						}
						srv.SetInventoryAt(10, 64, 0, 0, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1})
					}
					if mode == "flow" {
						servertest.SetFarmBlock(t, c, srv, 9, 64, 1, "minecraft:water", nil)
						servertest.SetFarmBlock(t, c, srv, 11, 64, 1, "minecraft:water", nil)
					}
					if mode == "bucket" {
						srv.GiveSlot("Camper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1})
						c.FacePlayer(10.5, 66, 1.5, 0, 90)
						c.Settle()
					}
					// Calibrate to block_change so the bucket's fluid_place cannot
					// mask the extinguish vibration in the same tick.
					srv.SetBlock(10, 64, 3, servertest.MustState(t, "minecraft:calibrated_sculk_sensor", map[string]string{"facing": "north"}))
					srv.SetBlock(10, 64, 4, servertest.MustState(t, "minecraft:target", map[string]string{"power": "11"}))
					servertest.HoldTicks(t, srv)
					heard := 0
					record := func(id int32, data []byte) {
						if id == protocol.IDPlaySoundEffect {
							name := servertest.SoundOf(data)
							if name == "minecraft:entity.generic.extinguish_fire" || name == "minecraft:block.candle.extinguish" {
								heard++
							}
						}
					}
					switch mode {
					case "bucket":
						servertest.UseHeld(c, 0, 90)
						c.SettleAndCollect(record)
					case "dispenser":
						srv.Dispense(10, 64, 0, protocol.FaceSouth)
					}
					servertest.RunTicks(t, srv, 10)
					c.SettleAndCollect(record)
					wantSounds := 0
					if lit == "true" {
						wantSounds = 1
					}
					if heard != wantSounds {
						t.Errorf("extinguish sounds = %d, want %d", heard, wantSounds)
					}
					tag, _ := srv.BlockEntityTagAt(10, 64, 3)
					want := 0
					if lit == "true" {
						want = 11
					} // vanilla: VibrationSystem, BLOCK_CHANGE
					if got, _ := nbt.IntField(tag, "last_vibration_frequency"); got != int32(want) {
						t.Errorf("sensor frequency = %d, want %d", got, want)
					}
				})
			}
		}
	}
}
