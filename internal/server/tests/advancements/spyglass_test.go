package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASpyglassLookedAtAGhastObtainsItsAdvancement: using a spyglass is a
// using_item each tick, and only a look that meets a ghast passes
// spyglass_at_ghast.
// vanilla: UsingItemTrigger, PlayerPredicate looking_at
func TestASpyglassLookedAtAGhastObtainsItsAdvancement(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:ghast", 8.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.GiveSlot("Watcher", player.HotbarSlot, servertest.ItemStack(t, "minecraft:spyglass", 1))
	const adv = "minecraft:adventure/spyglass_at_ghast"

	c.MoveTo(0.5, 64, 0.5, 90, 0)
	c.UseItemRightClicks(protocol.HandMain)
	flyTicks(c, srv, 5)
	if srv.AdvancementObtained("Watcher", adv, "spyglass_at_ghast") {
		t.Fatal("looking away from the ghast obtained the advancement")
	}
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	flyTicks(c, srv, 5)
	if !srv.AdvancementObtained("Watcher", adv, "spyglass_at_ghast") {
		t.Error("looking at the ghast through a spyglass did not obtain the advancement")
	}
}
