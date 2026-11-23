package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestPlacementInsideEffectCloud(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		item  registry.Item
		base  string
		face  int32
		typ   int32
		count func(*server.Server) int
	}{
		{"end crystal", registry.ItemEndCrystal, "minecraft:obsidian", protocol.FaceNorth, protocol.EntityTypeEndCrystal, (*server.Server).EndCrystalCount},
		{"armor stand", registry.ItemArmorStand, "minecraft:mycelium", protocol.FaceTop, protocol.EntityTypeArmorStand, (*server.Server).ArmorStandCount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "Placer"
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(3, 63, 5, registry.MustState(tc.base, nil))
			srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: int32(tc.item), Count: 2})
			c.MoveToGrounded(3.5, 64, 2.5, 0, 30, true)
			c.ChatCommand("summon area_effect_cloud 3.5 64 5.5 {Duration:12000,Radius:0.9f,RadiusPerTick:0f,WaitTime:0}")
			c.AwaitSpawn(protocol.EntityTypeAreaEffectCloud)

			c.PlaceBlock(3, 63, 5, tc.face)
			c.Settle()
			if got := tc.count(srv); got != 0 {
				t.Errorf("placed entities with an overlapping cloud = %d, want 0", got)
			}
			if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 2 {
				t.Errorf("held count with an overlapping cloud = %d, want 2", got)
			}
			if t.Failed() {
				return
			}

			// The cloud's radius of 0.9 clears the cell's east face from here.
			c.ChatCommand("tp @e[type=area_effect_cloud,limit=1] 5 64 5.5")
			servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
			c.PlaceBlock(3, 63, 5, tc.face)
			c.AwaitSpawn(tc.typ)
			c.Settle()
			if got := tc.count(srv); got != 1 {
				t.Errorf("placed entities after moving the cloud = %d, want 1", got)
			}
			if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
				t.Errorf("held count after moving the cloud = %d, want 1", got)
			}
		})
	}
}
