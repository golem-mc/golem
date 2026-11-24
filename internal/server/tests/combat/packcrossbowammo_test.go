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
			runFunction("typed", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{pickup:1b}"}}`) + "," +
			runFunction("arrowtype", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:arrow"}}`) + "," +
			runFunction("angled", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{ShotAtAngle:1b}"}}`)),
		"data/pk/enchantment/seq.json": spawnEnchantment(`{"effect":{"type":"minecraft:ignite","duration":100}},` +
			runFunction("afterignite", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Fire:2000s}"}}`)),
		"data/pk/enchantment/sky.json": spawnEnchantment(runFunction("sky", `,"requirements":{"type":"minecraft:location_check","predicate":{"can_see_sky":true}}`)),
		"data/pk/enchantment/moved.json": spawnEnchantment(runFunction("move", "") + "," +
			runFunction("aftermove", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:nbt":"{Pos:[5.5d,64.0d,0.5d]}"}}`)),
		"data/pack/function/sky.mcfunction":         "tag @s add sky\n",
		"data/pack/function/move.mcfunction":        "tp @s 5 64 0\n",
		"data/pack/function/aftermove.mcfunction":   "tag @s add aftermove\n",
		"data/pack/function/arrowtype.mcfunction":   "tag @s add arrowtype\n",
		"data/pack/function/angled.mcfunction":      "tag @s add angled\n",
		"data/pack/function/afterignite.mcfunction": "tag @s add afterignite\n",
		"data/pack/function/weapon.mcfunction":      "tag @s add weapon\n",
		"data/pack/function/typed.mcfunction":       "tag @s add typed\n",
		"data/pack/function/ammo.mcfunction": "tag @s add ammo\n" +
			"execute unless data entity @s {Fire:2000s} run tag @s add cold\n",
	})
}

// fireLoaded fires a crossbow enchanted with weapon and loaded with an item
// enchanted "ammo", returning the server after the shot.
// vanilla: ProjectileWeaponItem.shoot, Projectile.applyOnProjectileSpawned
func fireLoaded(t *testing.T, weapon, item string) *server.Server {
	t.Helper()
	return fireLoadedWith(t, weapon, item, "pk:ammo")
}

func fireLoadedWith(t *testing.T, weapon, item, ammoEnchantment string) *server.Server {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = ammoPack(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	crossbow := servertest.EnchantedStack(t, "minecraft:crossbow", weapon, 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.EnchantedStack(t, item, ammoEnchantment, 1)})
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
	for _, tag := range []string{"weapon", "ammo", "typed", "arrowtype"} {
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
	if !hasEntity(srv, "type=firework_rocket,tag=angled") {
		t.Error("the requirement on the rocket's saved shot angle did not pass")
	}
	if hasEntity(srv, "type=firework_rocket,tag=arrowtype") {
		t.Error("the arrow type requirement passed for the rocket")
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

// An entry reads the projectile after the earlier entries of the same stack ran.
// vanilla: Enchantment.applyEffects, Ignite.apply
func TestCrossbowSameStackRequirementSeesIgnite(t *testing.T) {
	srv := fireLoadedWith(t, "pk:weapon", "minecraft:arrow", "pk:seq")
	if !hasEntity(srv, "type=arrow,tag=afterignite") {
		t.Error("the requirement read the arrow before the ignite effect ran")
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
	servertest.HoldDraw(t, srv, "Archer", 4)
	servertest.ReleaseBow(c)
	c.Settle()
	for _, tag := range []string{"ammo", "typed", "arrowtype"} {
		if !hasEntity(srv, "type=arrow,tag="+tag) {
			t.Errorf("the bow's arrow was not tagged by the %s effect", tag)
		}
	}
}

// A location requirement reads the projectile's origin.
// vanilla: LocationCheck.test
func TestCrossbowSpawnEffectLocationRequirement(t *testing.T) {
	srv := fireLoadedWith(t, "pk:weapon", "minecraft:arrow", "pk:sky")
	if !hasEntity(srv, "type=arrow,tag=sky") {
		t.Error("the can_see_sky requirement failed for an arrow shot under open sky")
	}
}

// A rocket's requirement reads its position after the earlier entries ran.
// vanilla: Enchantment.applyEffects, EntityLocationPredicate.matches
func TestCrossbowRocketRequirementSeesMove(t *testing.T) {
	srv := fireLoadedWith(t, "pk:weapon", "minecraft:firework_rocket", "pk:moved")
	if !hasEntity(srv, "type=firework_rocket,tag=aftermove") {
		t.Error("the requirement read the rocket at its old position")
	}
}
