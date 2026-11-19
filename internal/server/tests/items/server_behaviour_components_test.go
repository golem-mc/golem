package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A stack's own glider, death_protection and damage_resistant, given with
// /give, act as the item defaults do: a chestplate or boots with a glider
// glide, a stick with death_protection saves its holder, and a diamond
// resistant to is_fire outlasts lava.

// giveHeld runs /give for the player and waits for the stack to land in the
// first hotbar slot, which it returns.
func giveHeld(t *testing.T, srv *server.Server, c *servertest.Client, name, item string) protocol.ItemStack {
	t.Helper()
	c.ChatCommand("give @s " + item)
	servertest.UntilTicks(t, srv, 20, "the given stack", func() bool {
		return !srv.InventorySlot(name, player.HotbarSlot).Empty()
	})
	return srv.InventorySlot(name, player.HotbarSlot)
}

// TestAGivenGliderOnAnyArmourSlotGlides: a worn stack with its own glider in
// the slot its equippable names starts and keeps a glide.
// vanilla: LivingEntity.canGlide, canGlideUsing
func TestAGivenGliderOnAnyArmourSlotGlides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		slot int
	}{
		{"diamond_chestplate[glider={}]", player.ArmorSlot + 1},
		{"leather_boots[glider={}]", player.ArmorSlot + 3},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			const name = "Glider"
			srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
			giveHeld(t, srv, c, name, tc.item)
			servertest.UseHeld(c, -90, 0)
			servertest.UntilTicks(t, srv, 20, "the piece worn", func() bool {
				return srv.InventorySlot(name, tc.slot).HasGlider()
			})

			c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
			servertest.HoldTicks(t, srv)
			servertest.StartFallFlying(c)
			var flags uint8
			seen := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
				if id != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != playerID {
					return false
				}
				m := servertest.ReadBurningMetadata(t, data)
				flags = m.Flags
				return true
			})
			if !seen {
				t.Fatal("no metadata for the glider after the wings were asked to open")
			}
			if flags&protocol.EntityFlagFlying == 0 {
				t.Fatalf("the glider's shared flags = %#02x, want the fall-flying bit %#02x", flags, protocol.EntityFlagFlying)
			}
			servertest.RunTicks(t, srv, 20)
			if !srv.PlayerFallFlying(name) {
				t.Error("the glide ended within 20 ticks")
			}
		})
	}
}

// TestAGivenDeathProtectionSavesItsHolder: a stick with its own
// death_protection is spent on a lethal blow, leaving its holder at one
// health. vanilla: LivingEntity.checkTotemDeathProtection
func TestAGivenDeathProtectionSavesItsHolder(t *testing.T) {
	t.Parallel()
	const name = "Holder"
	srv, c, playerID := servertest.JoinAs(t, servertest.SurvivalConfig(), name, servertest.OpsListing(t, name))
	giveHeld(t, srv, c, name, `stick[death_protection={death_effects:[{type:"minecraft:clear_all_effects"}]}]`)

	c.ChatCommand("damage @s 1000 minecraft:generic")
	saved := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == playerID && r.ReadU8() == protocol.EntityStatusTotem
	})
	if !saved {
		t.Fatal("no totem animation (entity event 35) after the lethal blow")
	}
	if got := srv.PlayerHealth(name); got != 1 {
		t.Errorf("health after the save = %v, want 1", got)
	}
	if got := srv.InventorySlot(name, player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand still holds %+v after the save", got)
	}
}
