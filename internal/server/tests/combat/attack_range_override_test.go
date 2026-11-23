package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAttackRangeOverride exercises a given stack's reach through the wire and
// a player-file reload, including the creative span and component removal.
// vanilla: AttackRange.CODEC, LivingEntity.getAttackRangeWith
func TestAttackRangeOverride(t *testing.T) {
	t.Parallel()
	cfg := servertest.CombatTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Reacher", servertest.OpsListing(t, "Reacher"))
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	give := func(item string) {
		t.Helper()
		c.ChatCommand("clear @s")
		c.Settle()
		c.ChatCommand("give @s " + item)
		var received protocol.ItemStack
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayContainerSlot {
				r := protocol.NewReader(data)
				r.ReadVarInt() // container
				r.ReadVarInt() // state
				if r.ReadI16() == player.HotbarSlot {
					var ok bool
					received, ok = r.ReadSlot()
					if !ok || r.Err() != nil {
						t.Fatal("the held stack did not read back")
					}
				}
			}
		})
		if got := srv.InventorySlot("Reacher", player.HotbarSlot); got.Empty() {
			t.Fatalf("/give %s left the hand empty", item)
		}
		// The packet reader must accept the component sent to the client too.
		if received.Empty() {
			t.Fatal("the client received no held stack")
		}
	}
	attack := func(distance float64, hurt bool) {
		t.Helper()
		cow := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5+distance)
		before := mobHealthNow(t, srv, cow)
		c.Attack(cow)
		c.Settle()
		if after := mobHealthNow(t, srv, cow); (after < before) != hurt {
			t.Errorf("attack at %g blocks: health %g -> %g, want hurt=%v", distance, before, after, hurt)
		}
	}

	give("diamond_sword[attack_range={max_reach:9,max_creative_reach:9}]")
	attack(7, true)
	before := srv.InventorySlot("Reacher", player.HotbarSlot)
	c.Disconnect()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Reacher"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	if len(items.Items) != 1 {
		t.Fatalf("saved inventory = %v, want one sword", items)
	}
	saved, ok := registry.StoredStack(items.Items[0].(nbt.Compound))
	if !ok || !saved.Equal(before) {
		t.Fatalf("saved stack = %+v (%v), want %+v", saved, ok, before)
	}
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c = servertest.DialServer(t, srv)
	c.Login("Reacher")
	c.Configure()
	var inventory []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayContainerContent {
			inventory = data
		}
	})
	c.MarkLoaded()
	if inventory == nil {
		t.Fatal("the rejoin sent no inventory")
	}
	c.Unread(servertest.PushedPacket{Id: protocol.IDPlayContainerContent, Data: inventory})
	if got := c.Inventory()[player.HotbarSlot]; !got.Equal(before) {
		t.Fatalf("rejoined held stack = %+v, want %+v", got, before)
	}
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	attack(7, true)

	give("diamond_sword[attack_range={max_reach:1,max_creative_reach:9}]")
	attack(7, false)
	c.ChatCommand("gamemode creative")
	c.Settle()
	attack(7, true)

	// A minimum beyond the target must also take precedence over the default.
	give("diamond_sword[attack_range={min_creative_reach:8,max_creative_reach:9}]")
	attack(2, false)
	c.ChatCommand("gamemode survival")
	c.Settle()
	give("diamond_sword[attack_range={min_reach:8,max_reach:9}]")
	attack(2, false)

	give("diamond_sword[!attack_range]")
	c.ChatCommand("attribute @s minecraft:entity_interaction_range base set 10")
	c.Settle()
	attack(7, true)
	c.ChatCommand("attribute @s minecraft:entity_interaction_range base set 3")
	c.Settle()
	attack(7, false)
	if srv.InventorySlot("Reacher", player.HotbarSlot).Removes(protocol.DataComponentAttackRange) {
		t.Error("the sword has no default attack_range to remove")
	}
}
