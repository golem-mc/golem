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
