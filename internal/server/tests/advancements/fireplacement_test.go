package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: FlintAndSteelItem.useOn, FireChargeItem.useOn
func TestFireStartingItemsObtainPlacedBlock(t *testing.T) {
	for _, target := range []string{"fire", "nether_portal", "campfire"} {
		for _, item := range []string{"flint_and_steel", "fire_charge"} {
			t.Run(target+"/"+item, func(t *testing.T) {
				cfg, data := packConfig(t)
				writePackFile(t, filepath.Join(data, "pk", "advancement", "placed.json"),
					`{"criteria":{"placed":{"trigger":"minecraft:placed_block","conditions":{"location":{"type":"minecraft:match_block","blocks":"minecraft:`+target+`"}}}}}`)
				srv, c := servertest.Join(t, cfg, "Lighter")
				servertest.HoldTicks(t, srv)
				c.MoveToGround(3.5, 64, 2.5, true)
				x, y, z := int32(3), int32(63), int32(3)
				switch target {
				case "nether_portal":
					servertest.BuildFrame(t, srv)
					x, y, z = servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ
				case "campfire":
					y = 64
					srv.SetBlock(x, y, z, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"}))
				default:
					srv.SetBlock(x, y, z, servertest.MustState(t, "minecraft:stone", nil))
				}
				srv.GiveSlot("Lighter", player.HotbarSlot, servertest.ItemStack(t, "minecraft:"+item, 1))
				c.PlaceBlock(x, y, z, protocol.FaceTop)
				c.Settle()
				servertest.RunTicks(t, srv, 2)

				placedY := y + 1
				if target == "campfire" {
					placedY = y
					if !registry.StateBool(srv.BlockAt(x, y, z), registry.PropLit) {
						t.Fatal("campfire was not lit")
					}
				}
				wantBlock := registry.BlockOf(servertest.MustState(t, "minecraft:"+target, nil))
				if got := registry.BlockOf(srv.BlockAt(x, placedY, z)); got != wantBlock {
					t.Fatalf("placed block = %v, want %s", got, target)
				}
				want := item == "flint_and_steel" && target != "campfire"
				if got := srv.AdvancementObtained("Lighter", "pk:placed", "placed"); got != want {
					t.Errorf("placed_block obtained = %t, want %t", got, want)
				}
			})
		}
	}
}
