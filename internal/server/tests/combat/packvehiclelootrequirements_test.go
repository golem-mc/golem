package combat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestPackLootRequirementsReadVehicleVictims tests that a blow's loot
// conditions read the NBT tags of a boat or a minecart: a stick's one point of
// damage ten times over stays under the 40 a vehicle takes before it breaks,
// and the enchantment's 10 more breaks it only when the vehicle is tagged.
// vanilla: Enchantment.damageContext, VehicleEntity.hurtServer
func TestPackLootRequirementsReadVehicleVictims(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	path := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/pk/enchantment/tagged.json": `{"anvil_cost":1,"description":{"text":"Tagged"},"max_level":1,"weight":1,
	"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
	"slots":["mainhand"],"supported_items":"minecraft:stick",
	"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":10},
	"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Tags:[\"a\"]}"}}}]}}`,
	} {
		file = filepath.Join(path, file)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Smith"
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:stick", "pk:tagged", 1))
	for _, kind := range []string{"oak_boat", "minecart"} {
		for _, tagged := range []bool{false, true} {
			tags := `{}`
			if tagged {
				tags = `{Tags:["a"]}`
			}
			c.ChatCommand("kill @e[type=!player]")
			c.ChatCommand("summon minecraft:" + kind + " 2.5 64 0.5 " + tags)
			c.Settle()
			var id int32
			if kind == "minecart" {
				for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
					id = cart.ID
				}
			} else if ids := srv.BoatIDs(); len(ids) > 0 {
				id = ids[0]
			}
			if id == 0 {
				t.Fatalf("%s did not spawn", kind)
			}
			servertest.AwaitFullSwing(t, srv, name)
			c.Attack(id)
			c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
			broke := !executeHolds(srv.RunLine("execute if entity @e[type=" + kind + "]"))
			if broke != tagged {
				t.Errorf("%s tagged=%v: broken=%v", kind, tagged, broke)
			}
		}
	}
}
