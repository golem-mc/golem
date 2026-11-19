package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Bees breed on flowers, follow one held out, and fly round what is in the
// way. vanilla: Bee.registerGoals, FlyingPathNavigation

// beeWithFlowers is beeWorld with a stack of dandelions in the player's hand.
func beeWithFlowers(t *testing.T) (*servertest.Client, *server.Server) {
	t.Helper()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.GiveSlot("Slayer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:dandelion"), Count: 16})
	return c, srv
}

func TestTwoFlowerFedBeesBreedABabyBee(t *testing.T) {
	t.Parallel()
	c, srv := beeWithFlowers(t)
	a := srv.SpawnMob(bee, 4.5, 64, 2.5)
	b := srv.SpawnMob(bee, 5.5, 64, 2.5)
	servertest.RunTicks(t, srv, 1)
	c.UseOn(a)
	c.UseOn(b)
	c.Settle()
	for range 40 {
		servertest.RunTicks(t, srv, 10)
		if len(srv.MobIDsOfKind(bee)) == 3 {
			break
		}
	}
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 3 {
		t.Fatalf("%d bees after feeding two flowers, want a baby among three", len(ids))
	}
	babies := 0
	for _, id := range ids {
		if srv.MobIsBaby(id) {
			babies++
		}
	}
	if babies != 1 {
		t.Fatalf("%d baby bees, want one", babies)
	}
}
