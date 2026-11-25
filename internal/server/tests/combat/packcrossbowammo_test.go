package combat

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
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

// replaceWith is a replace_block effect writing block at the projectile.
func replaceWith(block, requirements string) string {
	return `{"effect":{"type":"minecraft:replace_block","block_state":{"type":"minecraft:simple","state":{"id":"minecraft:` +
		block + `"}}}` + requirements + `}`
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
		"data/pk/enchantment/contend.json": spawnEnchantment(`{"effect":{"type":"minecraft:summon_entity","entity":"minecraft:armor_stand"}}`),
		"data/pk/enchantment/retarget.json": spawnEnchantment(runFunction("move", "") + "," +
			runFunction("originair", `,"requirements":{"type":"minecraft:location_check","predicate":{"block":{"blocks":"#minecraft:air"}}}`) + "," +
			runFunction("thisstone", `,"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:location":{"block":{"blocks":"#minecraft:base_stone_overworld"}}}}`) + "," +
			replaceWith("gold_block", "")),
		"data/pk/enchantment/restone.json": spawnEnchantment(
			replaceWith("stone", `,"requirements":{"type":"minecraft:location_check","predicate":{"block":{"blocks":"#minecraft:air"}}}`) + "," +
				runFunction("onstone", `,"requirements":{"type":"minecraft:location_check","predicate":{"block":{"blocks":"#minecraft:base_stone_overworld"}}}`)),
		"data/pack/function/onstone.mcfunction":     "tag @s add onstone\n",
		"data/pack/function/originair.mcfunction":   "tag @s add originair\n",
		"data/pack/function/thisstone.mcfunction":   "tag @s add thisstone\n",
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

func fireLoadedWith(t *testing.T, weapon, item, ammoEnchantment string, before ...func(*server.Server)) *server.Server {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = ammoPack(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	for _, f := range before {
		f(srv)
	}
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

// After a teleport from air onto stone, a location requirement still reads
// the enchantment's origin, an entity location requirement and a block
// effect read where the projectile now stands.
// vanilla: Enchantment.onProjectileSpawned, LocationCheck.test, EntityLocationPredicate.matches, ReplaceBlock.apply
func TestCrossbowSpawnEffectReplacesAtMovedProjectile(t *testing.T) {
	for _, item := range []struct{ name, typ string }{
		{"minecraft:arrow", "arrow"}, {"minecraft:firework_rocket", "firework_rocket"},
	} {
		srv := fireLoadedWith(t, "pk:weapon", item.name, "pk:retarget", func(srv *server.Server) {
			srv.RunLine("setblock 5 64 0 minecraft:stone")
		})
		if !hasEntity(srv, "type="+item.typ+",tag=originair") {
			t.Errorf("%s: the location requirement did not read the air the projectile was shot from", item.name)
		}
		if !hasEntity(srv, "type="+item.typ+",tag=thisstone") {
			t.Errorf("%s: the entity location requirement did not read the stone the projectile was moved to", item.name)
		}
		if !executeHolds(srv.RunLine("execute if block 5 64 0 minecraft:gold_block")) {
			t.Errorf("%s: no gold block where the projectile was moved to", item.name)
		}
	}
}

// A requirement reads the world as the previous entry left it.
// vanilla: Enchantment.applyEffects, LocationCheck.test
func TestCrossbowSpawnRequirementSeesPlacedBlock(t *testing.T) {
	srv := fireLoadedWith(t, "pk:weapon", "minecraft:arrow", "pk:restone")
	if !hasEntity(srv, "type=arrow,tag=onstone") {
		t.Error("the requirement did not see the stone the previous entry placed")
	}
}

// spawnStepper steps every projectile from the listener of the armor stand a
// spawn effect summons, while the shot's effects are still running, and
// reports whether a projectile moved.
type spawnStepper struct {
	srv     atomic.Pointer[server.Server]
	spawned atomic.Int32
	stepped atomic.Bool
	// gateway puts an end gateway at each projectile's cell, sending it to (8, 64, 8).
	gateway bool
}

func (p *spawnStepper) Enable(s plugin.Server) error {
	plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
		if e.Entity.Type() != registry.EntityTypeArmorStand {
			return
		}
		var shots []plugin.Entity
		var before []plugin.Vec3
		for body := range e.Entity.World().Entities() {
			if t := body.Type(); t == registry.EntityTypeArrow || t == registry.EntityTypeFireworkRocket {
				shots, before = append(shots, body), append(before, body.Pos())
			}
		}
		if p.gateway {
			for _, pos := range before {
				cell := plugin.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y)), Z: int(math.Floor(pos.Z))}
				e.Entity.World().SetBlockState(cell, plugin.DefaultState(registry.BlockEndGateway), plugin.UpdateAll)
				p.srv.Load().SetEndGatewayExit(int32(cell.X), int32(cell.Y), int32(cell.Z), 8, 64, 8, true)
			}
		}
		p.srv.Load().TickProjectiles()
		if p.gateway {
			p.srv.Load().WaitGatewayMoves()
		}
		for i, body := range shots {
			if body.Pos() != before[i] {
				p.stepped.Store(true)
			}
		}
		p.spawned.Add(1)
	})
	return nil
}

func (p *spawnStepper) Disable() {}

// A projectile's step that contends with its spawn effects waits for them:
// vanilla runs the shot's packet between ticks.
// vanilla: PacketUtils.ensureRunningOnSameThread, Projectile.applyOnProjectileSpawned
func TestCrossbowSpawnEffectsRunBeforeFirstStep(t *testing.T) {
	for _, item := range []string{"minecraft:arrow", "minecraft:firework_rocket"} {
		cfg := servertest.BowTestConfig()
		cfg.LevelName = ammoPack(t)
		stepper := &spawnStepper{gateway: item == "minecraft:arrow"}
		srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "stepper", Plugin: stepper}})
		stepper.srv.Store(srv)
		c := servertest.DialServer(t, srv)
		c.JoinAs("Archer")
		servertest.HoldTicks(t, srv)
		crossbow := servertest.EnchantedStack(t, "minecraft:crossbow", "pk:flame", 1)
		crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.EnchantedStack(t, item, "pk:contend", 1)})
		srv.GiveSlot("Archer", 36, crossbow)
		c.MoveTo(0.5, 64, 0.5, -90, 0)
		c.Settle()
		c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
			b.PutVarInt(protocol.HandMain)
			b.PutVarInt(1)
			b.PutF32(-90)
			b.PutF32(0)
		})
		// The listener runs on a tick once the connection has queued its call.
		for stepper.spawned.Load() == 0 {
			select {
			case <-srv.PluginCallsQueued():
			case <-time.After(10 * time.Second):
				t.Fatalf("%s: the spawn effect summoned nothing", item)
			}
			servertest.RunTicks(t, srv, 1)
		}
		c.Settle()
		if stepper.stepped.Load() {
			t.Errorf("%s: the projectile stepped before its spawn effects ran", item)
		}
		typ := "type=" + item[len("minecraft:"):]
		if arrows := srv.ArrowsInDimension("minecraft:overworld"); item == "minecraft:arrow" && (len(arrows) != 1 || !arrows[0].Burning) {
			t.Errorf("%s: arrows %+v, want one lit by the weapon's Flame", item, arrows)
		}
		get := "data get entity @e[" + typ + ",limit=1] Pos"
		pos := fmt.Sprint(srv.RunLine(get))
		servertest.RunTicks(t, srv, 1)
		if fmt.Sprint(srv.RunLine(get)) == pos {
			t.Errorf("%s: the projectile did not step once its spawn effects ran", item)
		}
		if stepper.gateway {
			srv.WaitGatewayMoves()
			if arrows := srv.ArrowsInDimension("minecraft:overworld"); len(arrows) != 1 || math.Abs(arrows[0].X-8.5) > 2 {
				t.Errorf("%s: arrows %+v, want the gateway to have moved the arrow after its effects", item, arrows)
			}
		}
	}
}
