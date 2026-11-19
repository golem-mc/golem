package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockItem.place, BucketItem.use, FlintAndSteelItem.useOn
func TestPlacedBlockMatchesThePlacementTool(t *testing.T) {
	for _, tc := range []struct {
		item, block, support, other string
		use                         bool
	}{
		{"stone", "stone", "stone", "dirt", false},
		{"wheat_seeds", "wheat", "farmland", "beetroot_seeds", false},
		{"flint_and_steel", "fire", "stone", "fire_charge", false},
		{"water_bucket", "water", "stone", "bucket", true},
		{"powder_snow_bucket", "powder_snow", "stone", "bucket", false},
		{"lily_pad", "lily_pad", "water", "frogspawn", true},
	} {
		t.Run(tc.item, func(t *testing.T) {
			cfg, data := packConfig(t)
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			for name, item := range map[string]string{"matching": tc.item, "different": tc.other} {
				writePackFile(t, filepath.Join(data, "pk", "advancement", name+".json"),
					`{"criteria":{"placed":{"trigger":"minecraft:placed_block","conditions":{"location":{"type":"minecraft:all_of","terms":[`+
						`{"type":"minecraft:match_block","blocks":"minecraft:`+tc.block+`"},`+
						`{"type":"minecraft:match_tool","predicate":{"items":"minecraft:`+item+`"}}]}}}}}`)
			}
			srv, c := servertest.Join(t, cfg, "Placer")
			servertest.HoldTicks(t, srv)
			srv.SetBlock(3, 63, 3, servertest.MustState(t, "minecraft:"+tc.support, nil))
			c.MoveTo(3.5, 65, 3.5, 0, 90)
			stack := servertest.ItemStack(t, "minecraft:"+tc.item, 1)
			if tc.item == "flint_and_steel" {
				// The final use must match before the tool breaks.
				stack.Damage = 63
			}
			srv.GiveSlot("Placer", player.HotbarSlot, stack)
			if tc.use {
				servertest.UseHeld(c, 0, 90)
			} else {
				c.PlaceBlock(3, 63, 3, protocol.FaceTop)
			}
			c.Settle()
			want := registry.BlockOf(servertest.MustState(t, "minecraft:"+tc.block, nil))
			if got := registry.BlockOf(srv.BlockAt(3, 64, 3)); got != want {
				t.Fatalf("placed block = %v, want %s", got, tc.block)
			}
			held := srv.InventorySlot("Placer", player.HotbarSlot)
			switch tc.item {
			case "water_bucket", "powder_snow_bucket":
				if held.ItemID != int32(registry.ItemBucket) {
					t.Errorf("hand after placement = %+v, want empty bucket", held)
				}
			default:
				if !held.Empty() {
					t.Errorf("hand after final use = %+v, want empty", held)
				}
			}
			if !srv.AdvancementObtained("Placer", "pk:matching", "placed") {
				t.Error("matching placement tool did not grant progress")
			}
			if srv.AdvancementObtained("Placer", "pk:different", "placed") {
				t.Error("different placement tool granted progress")
			}
		})
	}
}
