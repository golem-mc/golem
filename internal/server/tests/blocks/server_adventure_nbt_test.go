package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

var adventureNBTCases = []struct {
	name, block, pattern string
	want                 bool
}{
	{"empty hive", "beehive", "{}", true},
	{"matching id", "beehive", `{id:"minecraft:beehive"}`, true},
	{"wrong id", "beehive", `{id:"minecraft:chest"}`, false},
	{"no entity", "stone", "{}", false},
}

func TestAdventurePlacesWithMatchingBlockEntityNBT(t *testing.T) {
	t.Parallel()
	for _, tc := range adventureNBTCases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := adventureWorld(t, servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("setblock 2 63 0 " + tc.block)
			c.Settle()
			c.ChatCommand(fmt.Sprintf(`give @s stone[can_place_on={blocks:"minecraft:%s",nbt:%s}] 4`, tc.block, tc.pattern))
			c.Settle()
			if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 4 {
				t.Fatalf("given stack = %+v, want four stones", got)
			}
			c.PlaceBlock(2, 63, 0, protocol.FaceTop)
			c.SettleKeeping()
			c.ExpectWithinTicks(srv, 2, protocol.IDPlayAckBlockChange)
			c.Settle()
			wantState, wantCount := registry.BlockAir.DefaultState(), int32(4)
			if tc.want {
				wantState, wantCount = registry.BlockStone.DefaultState(), 3
			}
			if got := srv.BlockAt(2, 64, 0); got != wantState {
				t.Errorf("placed state = %d, want %d", got, wantState)
			}
			if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != wantCount {
				t.Errorf("server stack = %+v, want count %d", got, wantCount)
			}
		})
	}
}
