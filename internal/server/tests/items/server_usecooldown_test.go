package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemStack.releaseUsing, BowItem.releaseUsing, CrossbowItem.releaseUsing,
// TridentItem.releaseUsing, SpyglassItem.releaseUsing
func TestAGivenUseCooldownStartsOnRelease(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"bow", "crossbow", "trident", "spyglass"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			const name = "Archer"
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, item+`[use_cooldown={seconds:1}]`)
			srv.GiveSlot(name, player.HotbarSlot+1, protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 5})
			servertest.HoldTicks(t, srv)
			servertest.UseHeld(c, -90, 0)
			c.ExpectNoneWithinTicks(srv, 30, isUseCooldown)
			c.ReleaseUseItem()
			expectUseCooldown(t, srv, c, "minecraft:"+item, 20)
			servertest.UseHeld(c, -90, 0)
			c.ExpectNoneWithinTicks(srv, 1, isUseCooldown)
			if srv.BowTicks(name) != 0 {
				t.Fatal("another use started inside the cooldown")
			}
			if item == "crossbow" && len(srv.InventorySlot(name, player.HotbarSlot).ChargedProjectiles()) == 0 {
				t.Fatal("the loaded crossbow fired inside the cooldown")
			}
		})
	}
}

func isUseCooldown(id int32, _ []byte) bool { return id == protocol.IDPlayCooldown }

func expectUseCooldown(t *testing.T, srv *server.Server, c *servertest.Client, group string, ticks int32) {
	t.Helper()
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if !isUseCooldown(id, data) {
			return false
		}
		r := protocol.NewReader(data)
		gotGroup, gotTicks := r.ReadString(256), r.ReadVarInt()
		if r.Err() != nil || gotGroup != group || gotTicks != ticks {
			t.Fatalf("Set Cooldown = %q %d (%v), want %q %d", gotGroup, gotTicks, r.Err(), group, ticks)
		}
		return true
	}) {
		t.Fatalf("no Set Cooldown for %s", group)
	}
}
