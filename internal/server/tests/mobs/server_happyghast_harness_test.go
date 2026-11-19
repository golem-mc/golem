package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// harnessScene is a still grown happy ghast beside a survival player holding
// item.
func harnessScene(t *testing.T, item string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := wolfScene(t, "Rider")
	id := srv.SpawnMob("minecraft:happy_ghast", 4.5, 64, 0.5)
	srv.SetMobNoAI(id)
	giveHand(t, srv, "Rider", item, 1)
	return srv, c, id
}

func bodyItem(srv *server.Server, id int32) string {
	name, _ := registry.ItemName(srv.MobEquipment(id, "body").ItemID)
	if srv.MobEquipment(id, "body").Empty() {
		return ""
	}
	return name
}

// TestAHarnessGoesOnAGrownHappyGhastAndShearsTakeItOff: a held harness goes
// in the bare body slot, spent and heard, and watchers see it; shears take it
// off again, dropping it and wearing by one.
// vanilla: Equippable.equipOnTarget, Mob.attemptToShearEquipment
func TestAHarnessGoesOnAGrownHappyGhastAndShearsTakeItOff(t *testing.T) {
	t.Parallel()
	srv, c, id := harnessScene(t, "minecraft:blue_harness")
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if got := bodyItem(srv, id); got != "minecraft:blue_harness" {
		t.Fatalf("body = %q after the click, want the blue harness", got)
	}
	if !seen.heard("minecraft:entity.happy_ghast.equip") {
		t.Errorf("no equip sound in %v", seen.sounds)
	}
	if len(seen.body) != 1 || seen.body[0] != "minecraft:blue_harness" {
		t.Errorf("body equipment sent = %v, want the blue harness", seen.body)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); !got.Empty() {
		t.Errorf("the harness was not spent: hand holds %d", got.Count)
	}

	giveHand(t, srv, "Rider", "minecraft:shears", 1)
	c.UseOn(id)
	seen = watchWolf(t, c, srv, id, 2)
	if got := bodyItem(srv, id); got != "" {
		t.Fatalf("body = %q after shearing", got)
	}
	if !seen.heard("minecraft:entity.happy_ghast.unequip") {
		t.Errorf("no unequip sound in %v", seen.sounds)
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || it.Item == "minecraft:blue_harness"
	}
	if !found {
		t.Error("the sheared harness did not drop")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("shears damage = %d, want 1", got.Damage)
	}
}

// TestAGhastlingRefusesAHarness: the body slot is a grown happy ghast's
// alone, so a harness on a ghastling is neither worn nor spent.
// vanilla: HappyGhast.canUseSlot, HappyGhast.mobInteract
func TestAGhastlingRefusesAHarness(t *testing.T) {
	t.Parallel()
	srv, c, id := harnessScene(t, "minecraft:blue_harness")
	srv.SetMobBaby(id, true)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 2)
	if got := bodyItem(srv, id); got != "" {
		t.Errorf("a ghastling wears %q", got)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 1 {
		t.Errorf("hand holds %d harnesses after the click, want 1", got.Count)
	}
}
