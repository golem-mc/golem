package combat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// ammoPack writes a data pack with two enchantments whose projectile_spawned
// effect tags the projectile "weapon" or "ammo".
func ammoPack(t *testing.T) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "ammo")
	files := map[string]string{"pack.mcmeta": `{"pack":{"description":"ammo"}}`}
	for _, name := range []string{"weapon", "ammo"} {
		files["data/pk/enchantment/"+name+".json"] = `{
   "anvil_cost":1,"description":{"text":"` + name + `"},"max_level":1,"weight":1,
   "min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
   "slots":["any"],"supported_items":"#minecraft:enchantable/crossbow",
   "effects":{"minecraft:projectile_spawned":[{"effect":{"type":"minecraft:run_function","function":"pack:` + name + `"}}]}}`
		files["data/pack/function/"+name+".mcfunction"] = "tag @s add " + name + "\n"
	}
	for name, text := range files {
		path := filepath.Join(pack, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fireLoaded fires a crossbow enchanted "weapon" and loaded with an item
// enchanted "ammo", returning the server after the shot.
// vanilla: ProjectileWeaponItem.shoot, Projectile.applyOnProjectileSpawned
func fireLoaded(t *testing.T, item string) *server.Server {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = ammoPack(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	crossbow := servertest.EnchantedStack(t, "minecraft:crossbow", "pk:weapon", 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.EnchantedStack(t, item, "pk:ammo", 1)})
	srv.GiveSlot("Archer", 36, crossbow)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	return srv
}

func TestCrossbowArrowRunsAmmoAndWeaponSpawnEffects(t *testing.T) {
	srv := fireLoaded(t, "minecraft:arrow")
	for _, tag := range []string{"weapon", "ammo"} {
		if !executeHolds(srv.RunLine("execute if entity @e[type=arrow,tag=" + tag + "]")) {
			t.Errorf("the fired arrow was not tagged by the %s effect", tag)
		}
	}
}

func TestCrossbowFireworkRunsOnlyAmmoSpawnEffect(t *testing.T) {
	srv := fireLoaded(t, "minecraft:firework_rocket")
	if !executeHolds(srv.RunLine("execute if entity @e[type=firework_rocket,tag=ammo]")) {
		t.Error("the fired rocket was not tagged by the ammunition effect")
	}
	if executeHolds(srv.RunLine("execute if entity @e[type=firework_rocket,tag=weapon]")) {
		t.Error("the fired rocket was tagged by the weapon effect")
	}
}
