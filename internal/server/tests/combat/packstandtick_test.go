package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const goldTickBoots = `{"anvil_cost":1,"description":{"text":"Gold"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["feet"],"supported_items":"#minecraft:enchantable/foot_armor",
	"effects":{"minecraft:tick":[{"effect":{"type":"minecraft:replace_block","offset":[0,-1,0],
	"block_state":{"type":"minecraft:simple_state_provider","state":{"id":"minecraft:gold_block"}}}}]}}`

// A pack enchantment's tick effect runs each tick on a worn armor stand piece.
// vanilla: EnchantmentHelper.tickEffects
func TestPackTickEffectRunsOnAnArmorStand(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = packEnchantments(t, map[string]string{"data/pk/enchantment/gold.json": goldTickBoots})
	srv, c := servertest.Join(t, cfg, "Smith", servertest.OpsListing(t, "Smith"))
	t.Cleanup(srv.HoldChunk(5, 5))
	servertest.HoldTicks(t, srv)
	c.Settle()
	stone := servertest.BlockState(t, "minecraft:stone")
	air := servertest.BlockState(t, "minecraft:air")
	srv.SetBlock(5, 63, 5, stone)
	srv.SetBlock(5, 64, 5, air)
	srv.SpawnArmorStand(5.5, 64, 5.5)
	srv.TickArmorStands(2)
	c.ChatCommand(`item replace entity @e[type=minecraft:armor_stand,limit=1] armor.feet with iron_boots[enchantments={"pk:gold":1}]`)
	c.Settle()
	srv.TickArmorStands(1)
	gold, _ := registry.BlockByName("minecraft:gold_block")
	if got := registry.BlockOf(srv.BlockAt(5, 63, 5)); got != gold {
		t.Fatalf("block under the stand is %s, want gold_block", got.Name())
	}
	// The next ticks keep running it: put stone back and it is replaced again.
	srv.SetBlock(5, 63, 5, stone)
	srv.TickArmorStands(1)
	if got := registry.BlockOf(srv.BlockAt(5, 63, 5)); got != gold {
		t.Fatalf("a later tick left %s under the stand, want gold_block", got.Name())
	}
}
