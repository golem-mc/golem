package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestShearingASheepSwingsForTheActorAndOthers(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	id := c.JoinAs("Shearer")
	watcher.ExpectPlayerAdded("Shearer")
	c.Settle()
	servertest.HoldTicks(t, srv)
	sheep := srv.SpawnMob("minecraft:sheep", 2.5, 64, 0.5)
	srv.SetMobNoAI(sheep)
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemShears), Count: 1})
	servertest.RunTicks(t, srv, 30)
	c.UseOn(sheep)
	c.SettleKeeping()
	if got := srv.AttackTicks("Shearer"); got != 0 {
		t.Errorf("attack ticks after shearing = %d, want 0", got)
	}
	for _, viewer := range []*servertest.Client{watcher, c} {
		r := protocol.NewReader(viewer.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
		if got := r.ReadVarInt(); got != id {
			t.Errorf("swing entity = %d, want %d", got, id)
		}
		if got := r.ReadVarInt(); got != protocol.HandMain {
			t.Errorf("swing hand = %d", got)
		}
	}
	// A shorn sheep still consumes a shears click, without swinging or resetting.
	servertest.RunTicks(t, srv, 30)
	before := srv.AttackTicks("Shearer")
	c.UseOn(sheep)
	c.SettleKeeping()
	if got := srv.AttackTicks("Shearer"); got != before {
		t.Errorf("attack ticks after refused shear = %d, want %d", got, before)
	}
	for _, viewer := range []*servertest.Client{watcher, c} {
		viewer.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))
	}

}

// vanilla: AbstractCow.mobInteract, DyeItem.interactLivingEntity,
// NameTagItem.interactLivingEntity, Animal.mobInteract
func TestMobUseSwingOutcomes(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	id := c.JoinAs("User")
	watcher.ExpectPlayerAdded("User")
	c.Settle()
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		name, kind string
		item       registry.Item
		hand       int32
		self       bool
	}{
		{name: "milk", kind: "minecraft:cow", item: registry.ItemBucket},
		{name: "dye offhand", kind: "minecraft:sheep", item: registry.ItemRedDye, hand: protocol.HandOff},
		{name: "name", kind: "minecraft:cow", item: registry.ItemNameTag},
		{name: "feed", kind: "minecraft:cow", item: registry.ItemWheat, self: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mob := srv.SpawnMob(tc.kind, 2.5, 64, 0.5)
			srv.SetMobNoAI(mob)
			stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 2}
			if tc.item == registry.ItemNameTag {
				name := protocol.Text{Text: "Named"}
				stack.CustomName = &name
			}
			slot := player.HotbarSlot
			if tc.hand == protocol.HandOff {
				slot = player.OffHandSlot
			}
			srv.GiveSlot("User", slot, stack)
			servertest.RunTicks(t, srv, 30)
			c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
				b.PutVarInt(mob)
				b.PutVarInt(tc.hand)
				b.PutLpVec3(0, 0.8, 0)
				b.PutBool(false)
			})
			c.SettleKeeping()
			if got := srv.AttackTicks("User"); got != 0 {
				t.Errorf("attack ticks after use=%d, want 0", got)
			}
			for _, viewer := range []struct {
				c     *servertest.Client
				swing bool
			}{{watcher, true}, {c, tc.self}} {
				if !viewer.swing {
					viewer.c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))
					continue
				}
				r := protocol.NewReader(viewer.c.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
				if got := r.ReadVarInt(); got != id {
					t.Errorf("swing entity=%d, want %d", got, id)
				}
				if got := r.ReadVarInt(); got != tc.hand {
					t.Errorf("swing hand=%d, want %d", got, tc.hand)
				}
			}
		})
	}
}
