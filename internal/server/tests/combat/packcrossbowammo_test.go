package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// spawnEnchantment is an enchantment whose projectile_spawned entries are
// effects (a JSON list body).
func spawnEnchantment(effects string) string {
	return `{
   "anvil_cost":1,"description":{"text":"e"},"max_level":1,"weight":1,
   "min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
   "slots":["any"],"supported_items":"#minecraft:enchantable/crossbow",
   "effects":{"minecraft:projectile_spawned":[` + effects + `]}}`
}

func runFunction(name, requirements string) string {
	return `{"effect":{"type":"minecraft:run_function","function":"pack:` + name + `"}` + requirements + `}`
}

// ammoPack has the enchantments pk:weapon and pk:ammo, whose functions tag the
// projectile "weapon" or "ammo", and pk:flame, which ignites it for 100 s.
// The ammo function also tags an arrow that is not yet burning "cold"; "typed"
// is run only where a requirement on "this" matches the arrow.
func ammoPack(t *testing.T) string {
	return packEnchantments(t, map[string]string{
		"data/pk/enchantment/weapon.json": spawnEnchantment(runFunction("weapon", "")),
		"data/pk/enchantment/flame.json":  spawnEnchantment(`{"effect":{"type":"minecraft:ignite","duration":100}}`),
		"data/pk/enchantment/ammo.json": spawnEnchantment(runFunction("ammo", "") + "," +
			runFunction("typed", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{pickup:1b}"}}`)),
		"data/pack/function/weapon.mcfunction": "tag @s add weapon\n",
		"data/pack/function/typed.mcfunction":  "tag @s add typed\n",
		"data/pack/function/ammo.mcfunction": "tag @s add ammo\n" +
			"execute unless data entity @s {Fire:2000s} run tag @s add cold\n",
	})
}

// fireLoaded fires a crossbow enchanted with weapon and loaded with an item
// enchanted "ammo", returning the server after the shot.
// vanilla: ProjectileWeaponItem.shoot, Projectile.applyOnProjectileSpawned
func fireLoaded(t *testing.T, weapon, item string) *server.Server {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = ammoPack(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	crossbow := servertest.EnchantedStack(t, "minecraft:crossbow", weapon, 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.EnchantedStack(t, item, "pk:ammo", 1)})
	srv.GiveSlot("Archer", 36, crossbow)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	return srv
}

func hasEntity(srv *server.Server, selector string) bool {
	return executeHolds(srv.RunLine("execute if entity @e[" + selector + "]"))
}

func TestCrossbowArrowRunsAmmoAndWeaponSpawnEffects(t *testing.T) {
	srv := fireLoaded(t, "pk:weapon", "minecraft:arrow")
	for _, tag := range []string{"weapon", "ammo", "typed"} {
		if !hasEntity(srv, "type=arrow,tag="+tag) {
			t.Errorf("the fired arrow was not tagged by the %s effect", tag)
		}
	}
}

func TestCrossbowFireworkRunsOnlyAmmoSpawnEffect(t *testing.T) {
	srv := fireLoaded(t, "pk:weapon", "minecraft:firework_rocket")
	if !hasEntity(srv, "type=firework_rocket,tag=ammo") {
		t.Error("the fired rocket was not tagged by the ammunition effect")
	}
	if hasEntity(srv, "type=firework_rocket,tag=weapon") {
		t.Error("the fired rocket was tagged by the weapon effect")
	}
	if hasEntity(srv, "type=firework_rocket,tag=typed") {
		t.Error("the requirement on the arrow's saved data passed for the rocket")
	}
}

// The ammunition's function runs before the weapon's ignite effect.
// vanilla: Projectile.applyOnProjectileSpawned, Ignite.apply
func TestCrossbowFlameIgnitesAfterAmmoEffect(t *testing.T) {
	srv := fireLoaded(t, "pk:flame", "minecraft:arrow")
	if !hasEntity(srv, "type=arrow,tag=cold") {
		t.Error("the ammunition function saw the arrow already burning")
	}
	if !hasEntity(srv, "type=arrow,nbt={Fire:2000s}") {
		t.Error("the arrow was not ignited by the weapon")
	}
}

func TestBowArrowRunsAmmoSpawnEffect(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = ammoPack(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Archer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot("Archer", 37, servertest.EnchantedStack(t, "minecraft:arrow", "pk:ammo", 1))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	servertest.ExpectArrowSpawn(srv, c)
	for _, tag := range []string{"ammo", "typed"} {
		if !hasEntity(srv, "type=arrow,tag="+tag) {
			t.Errorf("the bow's arrow was not tagged by the %s effect", tag)
		}
	}
}
