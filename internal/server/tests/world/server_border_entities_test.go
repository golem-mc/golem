package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAttackAndInteractIgnoreATargetOutsideTheBorder checks that a player
// inside a 10-block border can neither hit nor milk a cow whose block column
// is outside it, while a cow inside takes the hit.
// vanilla: ServerGamePacketListenerImpl.handleAttack, handleInteract
func TestAttackAndInteractIgnoreATargetOutsideTheBorder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		cowX     float64
		outside  bool
		wantHurt bool
	}{
		// The border runs from -5 to 5, so block column 5 is outside.
		{"outside", 5.5, true, false},
		{"inside", 4.5, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Fencer", servertest.OpsListing(t, "Fencer"))
			c.MoveToGround(3.5, 64, 0.5, true)
			c.Settle()
			c.ChatCommand("worldborder set 10")
			c.Settle()

			id := srv.SpawnMob("minecraft:cow", tc.cowX, 64, 0.5)
			before, ok := srv.MobHealth(id)
			if !ok {
				t.Fatal("the cow did not spawn")
			}
			servertest.HoldTicks(t, srv)
			c.Attack(id)
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			after, ok := srv.MobHealth(id)
			if !ok {
				t.Fatal("the cow is gone after one hit")
			}
			if hurt := after < before; hurt != tc.wantHurt {
				t.Fatalf("cow health %v -> %v, hurt = %v, want %v", before, after, hurt, tc.wantHurt)
			}
			if !tc.outside {
				return
			}

			bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
			srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
			c.Settle()
			c.UseOn(id)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			if held := srv.InventorySlot("Fencer", player.HotbarSlot); held.ItemID != bucket {
				t.Errorf("held after milking a cow outside the border = %+v, want the bucket", held)
			}
		})
	}
}
