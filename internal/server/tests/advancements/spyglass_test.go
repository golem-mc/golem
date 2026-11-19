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

// TestASpyglassLookedAtAGhastBehindAWallObtainsNothing: looking_at needs a
// line of sight, so a ghast behind netherrack does not pass.
// vanilla: PlayerPredicate.matches, LivingEntity.hasLineOfSight
func TestASpyglassLookedAtAGhastBehindAWallObtainsNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	netherrack := servertest.BlockState(t, "minecraft:netherrack")
	for y := int32(62); y <= 70; y++ {
		for z := int32(-4); z <= 4; z++ {
			if !srv.SetBlock(4, y, z, netherrack) {
				t.Fatal("could not build the wall")
			}
		}
	}
	id := srv.SpawnMob("minecraft:ghast", 8.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.GiveSlot("Watcher", player.HotbarSlot, servertest.ItemStack(t, "minecraft:spyglass", 1))

	c.MoveTo(0.5, 64, 0.5, 90, 0)
	c.UseItemRightClicks(protocol.HandMain)
	flyTicks(c, srv, 5)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	flyTicks(c, srv, 5)
	if srv.AdvancementObtained("Watcher", "minecraft:adventure/spyglass_at_ghast", "spyglass_at_ghast") {
		t.Error("a ghast behind a wall obtained the advancement")
	}
}

// TestASpyglassLookedAtAGhastPastTallGrassObtainsItsAdvancement: a block
// without a collision shape does not break the line of sight.
// vanilla: LivingEntity.hasLineOfSight, ClipContext.Block.COLLIDER
func TestASpyglassLookedAtAGhastPastTallGrassObtainsItsAdvancement(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	grass := servertest.BlockState(t, "minecraft:short_grass")
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, grass) {
			t.Fatal("could not place the grass")
		}
	}
	id := srv.SpawnMob("minecraft:ghast", 8.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.GiveSlot("Watcher", player.HotbarSlot, servertest.ItemStack(t, "minecraft:spyglass", 1))

	c.MoveTo(0.5, 64, 0.5, 90, 0)
	c.UseItemRightClicks(protocol.HandMain)
	flyTicks(c, srv, 5)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	flyTicks(c, srv, 5)
	if !srv.AdvancementObtained("Watcher", "minecraft:adventure/spyglass_at_ghast", "spyglass_at_ghast") {
		t.Error("grass between the player and the ghast broke the line of sight")
	}
}
