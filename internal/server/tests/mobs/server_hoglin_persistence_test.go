package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// Feeding or breeding a hoglin, and a piglin holding anything but gold
// ingots, make it persistent.
// vanilla: Hoglin.mobInteract, Hoglin.getBreedOffspring, Piglin.holdInOffHand

func TestAHoglinFedCrimsonFungusIsPersistent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:crimson_fungus", 4)
	servertest.HoldTicks(t, srv)
	fed := srv.SpawnMob(hoglin, 1.5, 64, 2.5)
	unfed := srv.SpawnMob(hoglin, 6.5, 64, 6.5)
	c.Settle()
	if srv.MobPersistent(fed) || srv.MobPersistent(unfed) {
		t.Fatal("a fresh hoglin is persistent")
	}
	c.UseOn(fed)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if !srv.MobPersistent(fed) {
		t.Error("the fed hoglin is not persistent")
	}
	if srv.MobPersistent(unfed) {
		t.Error("the unfed hoglin is persistent")
	}
}
