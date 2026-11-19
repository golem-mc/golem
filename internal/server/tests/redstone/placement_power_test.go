package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BlockInput.place, DiodeBlock.onPlace, BlockBehaviour.onPlace
func TestSetblockWaitsForPowerNeighborChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block, property string
		door            bool
	}{
		{"redstone_lamp", "lit", false},
		{"oak_door", "powered", true},
		{"dispenser", "triggered", false},
		{"repeater[facing=north]", "powered", false},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.HoldTicks(t, srv)
			servertest.Setblock(t, c, 5, 64, 4, "redstone_block")
			servertest.Setblock(t, c, 5, 64, 5, tc.block)
			if tc.door {
				// Complete the door without giving the lower half a real
				// neighbour change before the assertion.
				c.ChatCommand("setblock 5 65 5 " + tc.block + "[half=upper] strict")
				c.Settle()
			}
			name, _ := servertest.StateProps(srv.BlockAt(5, 64, 5))
			expect := func(powered string) {
				t.Helper()
				props := map[string]string{tc.property: powered}
				if tc.door || tc.block == "oak_trapdoor" || tc.block == "oak_fence_gate" {
					props["open"] = powered
				}
				servertest.ExpectBlockNamed(t, srv, 5, 64, 5, name, props)
			}
			expect("false")
			servertest.RunTicks(t, srv, 8)
			expect("false")

			servertest.Setblock(t, c, 6, 64, 5, "stone")
			servertest.RunTicks(t, srv, 2)
			expect("true")
		})
	}
}

// vanilla: DiodeBlock.setPlacedBy, ComparatorBlock.shouldTurnOn
func TestHandPlacedDiodePowersAfterOneTick(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ item, source string }{
		{"repeater", "redstone_block"},
		{"comparator", "composter[level=8]"},
	} {
		t.Run(tc.item+"/"+tc.source, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.HoldTicks(t, srv)
			servertest.Setblock(t, c, 3, 64, 2, tc.source)
			servertest.PlaceFrom(c, "minecraft:"+tc.item, 3, 0, 0, 3, 63, 3, protocol.FaceTop)
			servertest.ExpectBlockNamed(t, srv, 3, 64, 3, "minecraft:"+tc.item, map[string]string{"powered": "false", "facing": "north"})
			servertest.RunTicks(t, srv, 1)
			servertest.ExpectBlockNamed(t, srv, 3, 64, 3, "minecraft:"+tc.item, map[string]string{"powered": "true"})
		})
	}
}
