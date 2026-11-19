package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func stateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s%v", name, props)
	}
	return id
}

// TestHarvestingHoneyOverACampfireObtainsSafelyHarvestHoney: a bottle filled
// from a full hive counts only when a lit campfire's smoke reaches the hive.
// vanilla: LocationPredicate.smokey, CampfireBlock.isSmokeyPos
func TestHarvestingHoneyOverACampfireObtainsSafelyHarvestHoney(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:glass_bottle")
	servertest.HoldTicks(t, srv)
	actor.MoveToGround(2.5, 64, 0.5, true)
	actor.Settle()
	full := stateOf(t, "minecraft:beehive", map[string]string{"facing": "north", "honey_level": "5"})
	srv.SetBlock(5, 64, 2, full)
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	if srv.AdvancementObtained("Actor", "minecraft:husbandry/safely_harvest_honey", "safely_harvest_honey") {
		t.Fatal("a bottle filled without smoke obtained safely_harvest_honey")
	}
	srv.SetBlock(5, 62, 2, stateOf(t, "minecraft:campfire", map[string]string{"facing": "north", "lit": "true", "signal_fire": "false", "waterlogged": "false"}))
	srv.SetBlock(5, 64, 2, full)
	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	actor.Settle()
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	if !srv.AdvancementObtained("Actor", "minecraft:husbandry/safely_harvest_honey", "safely_harvest_honey") {
		t.Error("a bottle filled over a lit campfire did not obtain safely_harvest_honey")
	}
}
