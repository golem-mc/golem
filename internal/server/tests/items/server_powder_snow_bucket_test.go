package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SolidBucketItem.useOn, BlockItem.useOn
func TestPowderSnowBucketTransformerFallback(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.GameMode = mode
			srv, c := servertest.Join(t, cfg, "Transformer", servertest.OpsListing(t, "Transformer"))
			servertest.HoldTicks(t, srv)
			for _, hand := range []int32{protocol.HandMain, protocol.HandOff} {
				for _, transforms := range []bool{true, false} {
					slot := player.HotbarSlot
					if hand == protocol.HandOff {
						slot = player.OffHandSlot
					}
					c.ChatCommand("clear @s")
					c.ChatCommand(`give @s powder_snow_bucket[block_transformer="minecraft:axe"]`)
					c.Settle()
					full := srv.InventorySlot("Transformer", player.HotbarSlot)
					if full.Empty() {
						t.Fatal("/give did not supply the bucket")
					}
					srv.GiveSlot("Transformer", player.HotbarSlot, protocol.ItemStack{})
					srv.GiveSlot("Transformer", slot, full)
					before, after := registry.BlockOakLog.DefaultState(), registry.BlockStrippedOakLog.DefaultState()
					if !transforms {
						before, after = registry.BlockStone.DefaultState(), registry.BlockStone.DefaultState()
					}
					srv.SetBlock(3, 63, 0, before)
					// Obstruct placement so the item's transformer fallback is tried.
					srv.SetBlock(3, 64, 0, registry.BlockStone.DefaultState())
					c.UseBlockWithHand(hand, 3, 63, 0)
					c.Settle()
					if got := srv.BlockAt(3, 63, 0); got != after {
						t.Errorf("hand=%d transforms=%v: block=%d, want %d", hand, transforms, got, after)
					}
					want := full
					if mode == "survival" && transforms {
						want = protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1}
					}
					if got := srv.InventorySlot("Transformer", slot); !got.Equal(want) {
						t.Errorf("hand=%d transforms=%v: server inventory=%+v, want %+v", hand, transforms, got, want)
					}
				}
			}
		})
	}
}

// TestAPowderSnowBucketPlacesTheBlockAndComesBackEmpty: Use Item On sets the
// block over the clicked face, the bucket empties, and the watcher hears the
// bucket's empty sound rather than the block's place sound.
// vanilla: BlockItem.place, BucketItem.getEmptySuccessItem
func TestAPowderSnowBucketPlacesTheBlockAndComesBackEmpty(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:powder_snow_bucket")

	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), registry.MustState("minecraft:powder_snow", nil); got != want {
		t.Errorf("the block over the click = %d, want powder snow %d", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:bucket") || got.Count != 1 {
		t.Errorf("the hand after placing = %+v, want one empty bucket", got)
	}
	var heard []servertest.PlaySoundEffect
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			heard = append(heard, servertest.DecodePlaySoundEffect(t, data))
		}
	})
	if len(heard) != 1 {
		t.Fatalf("the watcher heard %d sounds, want one item.bucket.empty_powder_snow alone", len(heard))
	}
	if name, _ := servertest.SoundEventName(heard[0].RegistryID); name != "minecraft:item.bucket.empty_powder_snow" {
		t.Errorf("the watcher heard %q, want item.bucket.empty_powder_snow", name)
	}
	if heard[0].Pitch != 0.8 || heard[0].Volume != 1.0 {
		t.Errorf("the sound volume/pitch = %v/%v, want 1.0/0.8", heard[0].Volume, heard[0].Pitch)
	}
}

// TestACreativePowderSnowBucketIsKept: creative has infinite materials, so
// the placed bucket stays full.
// vanilla: BucketItem.getEmptySuccessItem
func TestACreativePowderSnowBucketIsKept(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	full := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:powder_snow_bucket"), Count: 1}
	srv.GiveSlot("Builder", player.HotbarSlot, full)

	c.UseBlock(3, 63, 0)
	c.Settle()

	if got, want := srv.BlockAt(3, 64, 0), registry.MustState("minecraft:powder_snow", nil); got != want {
		t.Errorf("the block over the click = %d, want powder snow %d", got, want)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.ItemID != full.ItemID {
		t.Errorf("the creative hand after placing = %+v, want the full bucket", got)
	}
}

// TestADispenserPlacesPowderSnowOnlyIntoAir: the dispenser sets the block in
// the empty cell it faces and keeps an empty bucket; a filled cell refuses
// and drops the bucket.
// vanilla: SolidBucketItem.emptyContents
func TestADispenserPlacesPowderSnowOnlyIntoAir(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		before  uint16
		placed  bool
		slotOut string
	}{
		{"air", 0, true, "minecraft:bucket"},
		{"stone", registry.MustState("minecraft:stone", nil), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Watcher")
			if !srv.PlaceContainer(8, 70, 8, "minecraft:dispenser") {
				t.Fatal("the world did not take the dispenser")
			}
			srv.SetInventoryAt(8, 70, 8, 0, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:powder_snow_bucket"), Count: 1})
			srv.SetBlock(8, 70, 9, tc.before)
			c.Settle()

			srv.Dispense(8, 70, 8, protocol.FaceSouth)

			snow := registry.MustState("minecraft:powder_snow", nil)
			if got := srv.BlockAt(8, 70, 9); (got == snow) != tc.placed {
				t.Errorf("the cell after the dispense = %d, powder snow placed = %v, want %v", got, got == snow, tc.placed)
			}
			slots := srv.InventoryAt(8, 70, 8)
			if tc.placed {
				if len(slots) == 0 || slots[0].ItemID != servertest.ItemIDOrDie(t, tc.slotOut) {
					t.Errorf("the dispenser's slot = %+v, want %s", slots, tc.slotOut)
				}
			} else if len(slots) > 0 && !slots[0].Empty() {
				t.Errorf("the dispenser's slot = %+v, want the bucket dropped", slots[0])
			}
			c.Disconnect()
		})
	}
}
